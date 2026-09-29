package browser

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	pw "github.com/mxschmitt/playwright-go"
)

// PlaywrightDriver is Backend 1: playwright-go driving a real Chromium
// build. Resolution order: explicit ExecutablePath → Channel ("chrome")
// → cached playwright chromium → error telling the user to install one.
type PlaywrightDriver struct {
	// FindChrome locates a system Chrome; nil means default detection.
	FindChrome func() string
	// CachedChromium is the playwright-bundled build (zero download).
	CachedChromium string
}

func (d *PlaywrightDriver) executable(opts Options) (channel, exe string) {
	if opts.ExecutablePath != "" {
		return "", opts.ExecutablePath
	}
	if d.CachedChromium != "" {
		if _, err := os.Stat(d.CachedChromium); err == nil {
			return "", d.CachedChromium
		}
	}
	if opts.Channel != "" {
		return opts.Channel, ""
	}
	if d.FindChrome != nil {
		if p := d.FindChrome(); p != "" {
			return "", p
		}
	}
	for _, p := range []string{
		"/usr/bin/google-chrome", "/usr/bin/chromium",
		"/opt/google/chrome/chrome",
	} {
		if _, err := os.Stat(p); err == nil {
			return "", p
		}
	}
	return "chrome", ""
}

// Launch starts headless=new Chromium with a fresh per-request profile.
func (d *PlaywrightDriver) Launch(ctx context.Context, opts Options) (Session, error) {
	opts.Defaults()
	if opts.ProfileDir == "" {
		return nil, fmt.Errorf("browser: ProfileDir required")
	}
	if err := os.MkdirAll(opts.ProfileDir, 0o700); err != nil {
		return nil, fmt.Errorf("browser: profile dir: %w", err)
	}
	inst, err := pw.Run()
	if err != nil {
		return nil, fmt.Errorf("browser: playwright run (driver missing? run `autokey browser install`): %w", err)
	}
	channel, exe := d.executable(opts)
	args := []string{
		"--headless=new",
		"--disable-blink-features=AutomationControlled",
		"--disable-dev-shm-usage",
		"--no-first-run",
		"--no-default-browser-check",
	}
	vw := parseViewport(opts.Viewport)
	lopts := pw.BrowserTypeLaunchPersistentContextOptions{
		Headless:   pw.Bool(true),
		Args:       args,
		Viewport:   &pw.Size{Width: vw[0], Height: vw[1]},
		Locale:     pw.String(opts.Locale),
		TimezoneId: pw.String(opts.Timezone),
		UserAgent:  pw.String(realUA()),
	}
	if channel != "" {
		lopts.Channel = pw.String(channel)
	}
	if exe != "" {
		lopts.ExecutablePath = pw.String(exe)
	}
	if opts.ProxyURL != "" {
		lopts.Proxy = &pw.Proxy{Server: opts.ProxyURL}
	}
	bctx, err := inst.Chromium.LaunchPersistentContext(opts.ProfileDir, lopts)
	if err != nil {
		_ = inst.Stop()
		return nil, fmt.Errorf("browser: launch (channel=%q exe=%q): %w", channel, exe, err)
	}
	// Stealth init script: runs before page scripts on every navigation.
	if err := bctx.AddInitScript(pw.Script{Content: pw.String(stealthInitJS())}); err != nil {
		_ = bctx.Close()
		_ = inst.Stop()
		return nil, fmt.Errorf("browser: init script: %w", err)
	}
	pages := bctx.Pages()
	if len(pages) == 0 {
		_ = bctx.Close()
		_ = inst.Stop()
		return nil, fmt.Errorf("browser: no page in context")
	}
	return &pwSession{
		inst: inst, ctx: bctx, page: pages[0],
		opts: opts,
	}, nil
}

// pwSession adapts playwright types to the Session contract.
type pwSession struct {
	inst *pw.Playwright
	ctx  pw.BrowserContext
	page pw.Page
	opts Options
}

func (s *pwSession) Close() error {
	_ = s.ctx.Close()
	return s.inst.Stop()
}

func (s *pwSession) Goto(ctx context.Context, url string) error {
	_, err := s.page.Goto(url, pw.PageGotoOptions{WaitUntil: pw.WaitUntilStateDomcontentloaded})
	if err != nil {
		return fmt.Errorf("browser: goto %s: %w", url, err)
	}
	return s.pause(ctx)
}

func (s *pwSession) resolve(locs []Locator) (pw.Locator, error) {
	for _, l := range locs {
		sel, err := toSelector(l)
		if err != nil {
			continue
		}
		loc := s.page.Locator(sel)
		if n, err := loc.Count(); err == nil && n > 0 {
			return loc.First(), nil
		}
	}
	return nil, fmt.Errorf("browser: no element matched %d locators", len(locs))
}

// toSelector maps the label-first strategy onto playwright selectors.
func toSelector(l Locator) (string, error) {
	switch l.Kind {
	case "label":
		// Accessible name match; playwright resolves label→control.
		return fmt.Sprintf("label=%s", l.Value), nil
	case "text":
		return fmt.Sprintf("text=%s", l.Value), nil
	case "role":
		// "button[Submit]" or plain "button".
		if i := strings.Index(l.Value, "["); i >= 0 {
			return fmt.Sprintf("role=%s[name=%s]", l.Value[:i], strings.TrimSuffix(l.Value[i+1:], "]")), nil
		}
		return fmt.Sprintf("role=%s", l.Value), nil
	case "css":
		return l.Value, nil
	case "xpath":
		return "xpath=" + l.Value, nil
	default:
		return "", fmt.Errorf("browser: unknown locator kind %q", l.Kind)
	}
}

func (s *pwSession) Fill(ctx context.Context, locs []Locator, text string) error {
	loc, err := s.resolve(locs)
	if err != nil {
		return err
	}
	if err := loc.Click(); err != nil {
		return fmt.Errorf("browser: focus for fill: %w", err)
	}
	// Human timing: jittered per-keystroke delays via keyboard.Type.
	kb := s.page.Keyboard()
	for _, r := range text {
		if err := kb.Type(string(r)); err != nil {
			return fmt.Errorf("browser: type: %w", err)
		}
		if err := sleepJitter(ctx, s.opts.KeyJitter); err != nil {
			return err
		}
	}
	return s.pause(ctx)
}

func (s *pwSession) Click(ctx context.Context, locs []Locator) error {
	loc, err := s.resolve(locs)
	if err != nil {
		return err
	}
	if err := loc.ScrollIntoViewIfNeeded(); err != nil {
		return fmt.Errorf("browser: scroll: %w", err)
	}
	if err := sleepJitter(ctx, s.opts.StepPause); err != nil {
		return err
	}
	// Mouse-move before click: hover first for human pointer trail.
	if err := loc.Hover(); err != nil {
		return fmt.Errorf("browser: hover: %w", err)
	}
	if err := loc.Click(); err != nil {
		return fmt.Errorf("browser: click: %w", err)
	}
	return s.pause(ctx)
}

func (s *pwSession) Tick(ctx context.Context, locs []Locator) error {
	loc, err := s.resolve(locs)
	if err != nil {
		return err
	}
	checked, err := loc.IsChecked()
	if err != nil {
		return fmt.Errorf("browser: ischecked: %w", err)
	}
	if checked {
		return s.pause(ctx)
	}
	return s.Click(ctx, locs)
}

func (s *pwSession) Select(ctx context.Context, locs []Locator, label string) error {
	loc, err := s.resolve(locs)
	if err != nil {
		return err
	}
	_, err = loc.SelectOption(pw.SelectOptionValues{Labels: &[]string{label}})
	if err != nil {
		return fmt.Errorf("browser: select %q: %w", label, err)
	}
	return s.pause(ctx)
}

func (s *pwSession) WaitFor(ctx context.Context, locs []Locator, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	sels := make([]string, 0, len(locs))
	for _, l := range locs {
		sel, err := toSelector(l)
		if err != nil {
			continue
		}
		sels = append(sels, sel)
	}
	if len(sels) == 0 {
		return fmt.Errorf("browser: no valid locators")
	}
	// Round-robin across locators: a single never-matching selector
	// must not consume the whole deadline (first match wins).
	for time.Now().Before(deadline) {
		for _, sel := range sels {
			loc := s.page.Locator(sel)
			if err := loc.WaitFor(pw.LocatorWaitForOptions{
				State:   pw.WaitForSelectorStateVisible,
				Timeout: pw.Float(500),
			}); err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("browser: wait timed out after %s", timeout)
}

func (s *pwSession) Exists(ctx context.Context, locs []Locator) (bool, error) {
	_, err := s.resolve(locs)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (s *pwSession) Text(ctx context.Context, locs []Locator) (string, error) {
	loc, err := s.resolve(locs)
	if err != nil {
		return "", err
	}
	t, err := loc.InnerText()
	if err != nil {
		return "", fmt.Errorf("browser: text: %w", err)
	}
	return t, nil
}

func (s *pwSession) Screenshot(ctx context.Context, name string) (string, error) {
	// Mask secrets pre-capture: password + PAN-like inputs get type=hidden
	// rendering so pixels never contain credentials.
	_, _ = s.page.Evaluate(`() => {
		for (const el of document.querySelectorAll('input[type=password],input[data-pan],input[data-secret]')) {
			el.setAttribute('type', 'hidden');
		}
	}`)
	path := filepath.Join(s.opts.RunDir, name+".png")
	if s.opts.RunDir != "" {
		_ = os.MkdirAll(s.opts.RunDir, 0o700)
	}
	_, err := s.page.Screenshot(pw.PageScreenshotOptions{Path: pw.String(path), FullPage: pw.Bool(true)})
	if err != nil {
		return "", fmt.Errorf("browser: screenshot: %w", err)
	}
	return path, nil
}

func (s *pwSession) pause(ctx context.Context) error {
	return sleepJitter(ctx, s.opts.StepPause)
}

func sleepJitter(ctx context.Context, bounds [2]time.Duration) error {
	if bounds[1] <= bounds[0] {
		bounds[1] = bounds[0] + time.Millisecond
	}
	span := bounds[1] - bounds[0]
	n, err := rand.Int(rand.Reader, big.NewInt(int64(span)+1))
	if err != nil {
		return err
	}
	d := bounds[0] + time.Duration(n.Int64())
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func parseViewport(v string) [2]int {
	parts := strings.Split(v, "x")
	if len(parts) != 2 {
		return [2]int{1366, 768}
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return [2]int{1366, 768}
	}
	return [2]int{w, h}
}

// realUA returns a current stable Chrome UA (bumped per release).
func realUA() string {
	return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
}

// stealthInitJS runs before page scripts: removes automation tells.
// Kept minimal and version-pinned; verify against target in spike.
func stealthInitJS() string {
	return `(function() {
  try {
    Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
    Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
    Object.defineProperty(navigator, 'platform', { get: () => 'Linux x86_64' });
    if (!window.chrome) { window.chrome = { runtime: {} }; }
    const origQuery = window.navigator.permissions && window.navigator.permissions.query;
    if (origQuery) {
      window.navigator.permissions.query = (p) => {
        if (p && p.name === 'notifications') {
          return Promise.resolve({ state: Notification.permission });
        }
        return origQuery(p);
      };
    }
    Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => 8 });
    Object.defineProperty(navigator, 'deviceMemory', { get: () => 8 });
  } catch (e) { /* stealth is best-effort */ }
})();`
}
