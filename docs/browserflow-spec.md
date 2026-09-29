# Spec: browserFlow + omegameta (#1)

Parent: `grand-plan.md` (frozen core + Addenda A/B). This spec changes
nothing in the grand plan; it details implementation. Any conflict →
grand plan wins.

Status: planning/implementing. No live runs against omegameta until the
user approves the probe. Backend unfinished → OTP path is blind-built,
user tests manually.

## 1. Glossary (locked names)

- `browserFlow` — framework: per-request automated browser runs.
- `omegameta` — first service profile (internal employee platform).
- slug — local-part of the request email; unique link key for
  OTP/cards/keys per request.
- No other service names exist yet. Do not invent any.

## 2. Service registry (dynamic domain, B2)

- Config: `services.<name>.base_url` (http/https, validated at set time).
- CLI: `autokey services domain [name] [url]` (plural — `service`
  singular stays systemd). Show current when args omitted.
- Profile struct: `{Name, BaseURL, Steps, Selectors}` — domain never in code.
- Reminder: when devops publishes production, run
  `autokey services domain omegameta <prod-url>` then test manually.

## 3. Locator strategy (B3, hard requirement)

Order per element: accessible-label → visible text → role →
css → xpath. First match wins. IDs/classes are last resort.
Profiles declare ordered fallback lists per element. A selector
health-check dry-run walks every step and fails fast before any
signup submit. UI change → update profile data, never framework code.

## 4. Driver (`internal/browser`)

- Interface `Driver`: Launch(ctx, opts) / NewContext(profileDir) /
  Goto / Fill / Click / WaitFor / Screenshot / Close.
- Backend 1: playwright-go, system Chrome channel
  (`Channel: "chrome"`, `ExecutablePath` fallback). Zero download when
  Chrome exists. Headless (`headless=new`).
- Backend 2 (shape only now): CDP-direct over localhost:9222 —
  no Node, no download. Activated only if spike says playwright fails.
- Stealth per request: fresh profile dir, real UA + Accept-Language,
  1366×768 viewport, en-US locale/timezone, languages/platform spoof,
  WebGL vendor/renderer spoof, chrome.runtime stub removal.
- Human timing: 80–250ms jittered keys, 300–900ms step pauses,
  mouse-move before click, scroll-into-view. Keyboard fill only,
  never evaluate-fill. No fixed sleeps for state (wait on DOM).
- Proxy (optional, browser egress only): `browser.proxy_url` config +
  `--proxy` per run + setup prompt. API/mail/direct stay direct.
  No credentials now — pluggable later.

## 5. Orchestrator (`internal/browserflow`)

- `Producer` implements `flow.KeyProducer` — zero changes to
  `details.go`, hook, or pool.
- Job model: `202 Accepted {request_id}` immediately; result via
  `GET /v1/requests/{id}`; live via `GET /v1/requests/{id}/stream`
  (chunked `data: {step,msg}`).
- Resume: steps idempotent-by-check (signed-in→skip, card-present→skip,
  keys-exist→return). Crash → re-POST resumes from persisted step state.
  Claim released on every exit path.
- Parallel: per-request browser contexts; pool Claim serializes cards;
  `browserflow.max_parallel` default 1, raise deliberately.
- Per-step structured log (`request_id, step, ms, ok, detail`) → hook
  JSONL + `request_steps` table (dashboard-visible).
- Screenshots on failure to `~/.autoApiKeys/runs/{req}/`; mask
  password/PAN inputs pre-capture; test asserts no 13–19-digit runs
  in log lines.

## 6. CAPTCHA/blocker recovery (configurable, default pause-manual)

- `browserflow.captcha_strategy`: `pause-manual` (default) |
  `abort-quarantine` | `backoff-retry`. Flag `--captcha-strategy`,
  setup prompt. No solvers, ever.
- pause-manual: hold job, dashboard/CLI resume.
- abort-quarantine: screenshot, mark `blocked-captcha`, release card,
  surface in dashboard.
- backoff-retry: fresh profile/IP, capped attempts.

## 7. Secrets (`request_accounts`, migration 004)

- Table: `request_id PK, slug, password_enc, dob, country, otp_used,
  created_at`. AES-256-GCM via existing `custom` envelope +
  `CUSTOM_CARD_KEY`. Decrypt command for debugging (mirror
  `cards show`). Never in hook JSONL (only last4/slug in logs).
- Password: `crypto/rand` 20 chars (upper/lower/digit/symbol), random
  per account. DOB: today − 20y via injectable clock.

## 8. omegameta profile (`internal/services/omegameta`)

- Prepare: slug = request email local-part (assert match at start);
  DOB today−20y; password random; country from card record
  (`--country` on `cards add`, default `US`; migration adds
  `custom_cards.country` + `cards.country` display column).
- Signup password-default: email → continue → password fields →
  remember-me tick if present → submit. Log each step.
- OTP fallback (blind-built, user tests): only if password path
  rejects; `WaitForOTP(email, hardened timeout)` concurrently with
  page load (buffer result); resend-once on timeout, then fail.
  Backend unfinished → expect this path broken until devops ships.
- Billing: billing page → add payment method → country → PAN/expiry/
  CVV/name → save → wait confirmation token on DOM.
- Keys: API-keys page → create × qty → collect → return. Persistence
  via unchanged `tryCard` writes.
- DOB widget modes: profile declares `dob_mode`; try selects-first.

## 9. CLI + setup

- `cards add --country US` (optional, default USA); stored per card.
- `autokey services domain [name] [url]` (new plural group).
- Setup prompts: browser proxy URL (optional), captcha strategy
  (default pause-manual), max_parallel (default 1).
- Hook routes: `GET /v1/requests/{id}`, `GET /v1/requests/{id}/stream`
  (WriteTimeout 0 on stream route only).

## 10. Tests (no live hits in CI)

- Table-driven: brand/DOB/country validation, OTP race buffer,
  step-resume matrix, locator fallback order, log PAN-scrub assert.
- Fixture: local HTML fixture page exercising the full
  signup→billing→keys flow through the real driver (headless shell).
- mail.WaitForOTP reuse covered by existing mail tests; hardened
  timeout path gets a unit test with injected clock/poller.

## 11. Pitfalls (from grand plan, enforced here)

1. Slug↔email match asserted at run start.
2. OTP waiter starts with page load (race buffer).
3. DOB declared per profile; selects-first.
4. Country flag + column; USA default.
5. max_parallel default 1.
6. Post-submit OTP challenge = expected branch, not failure.
7. Mask secrets pre-screenshot; PAN-scrub log test.
8. Backend bugs are platform states (retryable/blocked), never our bug.
