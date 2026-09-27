package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var (
	neonPink  = lipgloss.Color("#ff2ea6")
	neonCyan  = lipgloss.Color("#00e5ff")
	neonLime  = lipgloss.Color("#b6ff00")
	warnAmber = lipgloss.Color("#ffb000")
	errRed    = lipgloss.Color("#ff3b3b")
	dimGray   = lipgloss.Color("#8a8a8a")
)

func tty() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Banner prints the neon cyberpunk header on TTY only.
func Banner() {
	if !tty() {
		return
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(neonPink).Render("AUTOKEY")
	sub := lipgloss.NewStyle().Foreground(neonCyan).Render("wildcard inbox // card pool // key pipeline")
	fmt.Printf("%s  %s\n", title, sub)
}

// Ok prints a success line.
func Ok(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(neonLime).Render("✔ " + s)
	}
	fmt.Println(s)
}

// Info prints an informational line.
func Info(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(neonCyan).Render("» " + s)
	}
	fmt.Println(s)
}

// Warn prints a warning line (also for log parity).
func Warn(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(warnAmber).Render("⚠ " + s)
	} else {
		s = "WARN: " + s
	}
	fmt.Fprintln(os.Stderr, s)
}

// Err prints an error line.
func Err(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(errRed).Render("✖ " + s)
	} else {
		s = "ERROR: " + s
	}
	fmt.Fprintln(os.Stderr, s)
}

// Dim prints a dim line.
func Dim(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(dimGray).Render(s)
	}
	fmt.Println(s)
}

// PromptSecret reads one line from the terminal without echoing it.
// Falls back to a visible prompt when stdin is not a TTY.
func PromptSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		var line string
		if _, err := fmt.Scanln(&line); err != nil {
			return "", fmt.Errorf("read input: %w", err)
		}
		return strings.TrimSpace(line), nil
	}
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// previewMaskDelay is how long the last typed group stays visible.
const previewMaskDelay = 2 * time.Second

// PromptCardNumber reads a card number with live preview: each digit group
// stays visible for previewMaskDelay, then collapses to "*". Older groups
// are always masked, so only the group being typed is ever on screen.
// On submit the full number echoes as "…" plus the last 4 for confirmation.
func PromptCardNumber(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		var line string
		if _, err := fmt.Scanln(&line); err != nil {
			return "", fmt.Errorf("read input: %w", err)
		}
		return strings.TrimSpace(line), nil
	}
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", fmt.Errorf("raw terminal: %w", err)
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	var digits []rune
	redraw := func() {
		// Clear to line start, then render: masked groups + live tail.
		fmt.Fprint(os.Stderr, "\r\033[K"+prompt)
		n := len(digits)
		if n == 0 {
			return
		}
		full := n / 4
		for g := 0; g < full; g++ {
			fmt.Fprint(os.Stderr, "**** ")
		}
		fmt.Fprint(os.Stderr, string(digits[full*4:]))
	}
	// maskTimer collapses the live tail after the delay.
	var maskTimer *time.Timer
	armTimer := func() {
		if maskTimer != nil {
			maskTimer.Stop()
		}
		maskTimer = time.AfterFunc(previewMaskDelay, func() {
			fmt.Fprint(os.Stderr, "\r\033[K"+prompt)
			for i := 0; i < len(digits); i += 4 {
				if i > 0 {
					fmt.Fprint(os.Stderr, " ")
				}
				end := i + 4
				if end > len(digits) {
					end = len(digits)
				}
				fmt.Fprint(os.Stderr, strings.Repeat("*", end-i))
			}
		})
	}

	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return "", fmt.Errorf("read input: %w", err)
		}
		b := buf[0]
		switch {
		case b == '\r' || b == '\n':
			if maskTimer != nil {
				maskTimer.Stop()
			}
			last4 := ""
			if len(digits) >= 4 {
				last4 = string(digits[len(digits)-4:])
			}
			fmt.Fprintf(os.Stderr, "\r\033[K%s… (last4=%s)\n", prompt, last4)
			return string(digits), nil
		case b == 127 || b == 8: // backspace
			if len(digits) > 0 {
				digits = digits[:len(digits)-1]
				redraw()
				armTimer()
			}
		case b == 3: // Ctrl-C
			fmt.Fprintln(os.Stderr, "^C")
			return "", fmt.Errorf("cancelled")
		case b == 21: // Ctrl-U clears the line
			digits = nil
			redraw()
			if maskTimer != nil {
				maskTimer.Stop()
			}
		case b >= '0' && b <= '9':
			if len(digits) < 19 {
				digits = append(digits, rune(b))
				redraw()
				armTimer()
			}
		case b == ' ' || b == '-':
			// ignore separators; grouping is derived from digit count
		}
	}
}
