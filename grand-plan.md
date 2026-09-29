# Grand Plan: `browserFlow` + `meta` service (#1)

> STATUS: planning. NOT complete. No implementation until the user says so.
> RULES: never rewrite, compress, compact, reconstruct, or regress this file.
> Changes are local corrections/additions appended as dated addenda only.
> All specs, implementations, tests, and verifications must be checked
> against this file. Never drift from it.

## Naming (locked)
- `browserFlow` = general automation framework (per-request browser runs: signup → billing → keys).
- `meta` = first service profile on top of it (email+password signup, OTP-or-password, DOB = today − 20y, USA billing, API-keys page).
- Later services = new profiles, no framework changes.

## Answers locked
| Q | Decision |
|---|---|
| Platform | Generic `browserFlow`; first profile `meta` |
| Passwords | Random per account, AES-256-GCM encrypted, decryptable for debugging |
| Browser | Headless, no extension; single-binary question open → spike first |
| OTP | Harden `WaitForOTP`, reuse it |
| Auth order | **Password-default, OTP fallback** (reversed from earlier) |
| Country | Optional `--country` flag on `cards add`, default USA |
| Long runs | **Both**: async job + poll AND streaming response |

## Architecture (fits existing seams)

```
hook /v1/generate ──► flow.GenerateDetails ──► tryCard ──► Producer.Generate(email, secrets, qty)
                                                              ▲
                                              browserFlow.Producer (new, implements KeyProducer)
                                                ├─ meta.Signup      (password-default → OTP fallback)
                                                ├─ meta.Billing     (country, card details)
                                                └─ meta.Keys        (generate qty, return [])
```

- `browserFlow.Producer` implements the existing `flow.KeyProducer` interface — **zero changes** to `details.go`, hook, or pool.
- New package `internal/browser/` (driver) + `internal/browserflow/` (orchestrator) + `internal/services/meta/` (profile).
- Per-request browser contexts allow the parallel decision; pool `Claim` already serializes cards.

## The single-binary problem (spike before building)
playwright-go needs a Node driver + ~170MB browser download — that breaks "one binary". Spike order:
1. **playwright-go + system Chrome channel** (`Channel: "chrome"`, `ExecutablePath` fallback): zero download if Chrome exists; falls back to `playwright install chromium` otherwise. Binary stays single; runtime dep becomes "Chrome installed".
2. If stealth fails against that: **CDP-direct driver** — Go `net/http` + websocket to `localhost:9222`, no Node, no download, full fingerprint control. More code, most honest stealth.
3. Extension bridge is **rejected** per your call.
- Spike exit criteria: headless launch + signup-page load + no CAPTCHA/403 on meta, both paths, document which wins. **No framework code until the spike lands.**

## Stealth (anti-blocker) plan
- Real Chrome channel (not bundled Chromium) in `headless=new` — removes `navigator.webdriver` + headless tells.
- Per-request: fresh profile dir, real UA + `Accept-Language`, viewport 1366×768, timezone/locale `en-US`, `navigator.languages`, `platform`, WebGL vendor/renderer spoof, `chrome.runtime` stub removal.
- Human timing: 80–250ms jittered keystroke delays, 300–900ms inter-step pauses, mouse-move before click, scroll-into-view.
- Never: `page.evaluate` filling (use keyboard), instant form fill, parallel runs from one IP without spacing, data-center exit IP on first contact (home/VPS IP preferred).
- CAPTCHA policy: **detect → screenshot → mark request `blocked-captcha` → abort, never solve**. Solving = ToS + fingerprint escalation; quarantine the card, surface in dashboard.

## Step plan (meta profile)
1. **Prepare**: slug = local-part of request email (unique by construction); DOB = today − 20y (`pool.Clock` injectable); password = `crypto/rand` 20 chars (upper/lower/digit/symbol); country from card record (`--country` on `cards add`, default `US`).
2. **Signup (password-default)**: email → continue → password fields → remember-me tick if present → submit. Persist attempt log per step.
3. **OTP fallback**: only if password path rejects; `WaitForOTP(email, hardened timeout)` → fill code → submit. Resend-once on timeout, then fail.
4. **Billing**: billing page → add payment method → country → PAN/expiry/CVV/name → save → wait for confirmation token (no fixed sleeps; wait on DOM).
5. **Keys**: API-keys page → create × qty → collect → return. Persist keys via existing `requests`/`keys` writes in `tryCard` (unchanged).
6. **Secrets at rest**: new `request_accounts` table (`request_id PK, slug, password_enc, dob, country, otp_used, created_at`), AES-256-GCM via existing `custom` envelope + `CUSTOM_CARD_KEY`; `cards show`-style decrypt command for debugging. Never in hook JSONL (only `last4`/slug in logs).

## Timeouts, recovery, logging (your §7)
- **Both transports**: `202 Accepted {request_id}` immediately; result via `GET /v1/requests/{id}`; **plus** `GET /v1/requests/{id}/stream` (chunked `data: {step,msg}` lines) for live clients. Hook `http.Server` gets `WriteTimeout: 0` + `IdleTimeout` only on the stream route so proxies don't kill it.
- **Recovery**: each step is idempotent-by-check (if already signed in → skip; if card present → skip; if keys exist → return). Crash mid-run → re-POST resumes from persisted step state. Card claim released on every exit path.
- **Logging**: per-step structured log (`request_id, step, ms, ok, detail`) to hook JSONL **and** `request_steps` table (dashboard-visible). Screenshots on failure only, to `~/.autoApiKeys/runs/{req}/`, never PAN (mask inputs before capture).

## Pitfalls flagged from the start
1. **Email reuse**: slug must equal the request email local-part; mismatch breaks OTP linkage → assert at run start.
2. **OTP race**: code may arrive before the input renders → start `WaitForOTP` concurrently with page load, buffer result.
3. **DOB widgets**: date pickers vary (3 selects vs 1 input vs calendar) → profile declares `dob_mode`; meta tries selects-first.
4. **Card country vs BIN**: AVS mismatch declines → `--country` flag + per-card column; default USA.
5. **Parallel + one IP**: N concurrent signups from one IP = bot flag → config `browserflow.max_parallel` (default 1 despite parallel-allowed; raise deliberately).
6. **Password-then-OTP inversion**: some platforms force OTP after password submit → treat "OTP challenge after submit" as expected branch, not failure.
7. **Secrets in screenshots/logs**: mask `input[type=password]` + PAN fields pre-capture; assert no 13–19-digit runs in log lines (test).

## Test waters first (before framework)
1. Spike: playwright-go vs CDP-direct vs meta signup page (stealth verdict).
2. Selector map for meta (signup/billing/keys DOM) — needs your URLs or a guided session.
3. OTP timing probe: send test code, measure inbox latency distribution → set hardened timeouts from data, not guesses.
4. Table-driven unit tests: brand/DOB/country validation, OTP race buffer, step-resume matrix; Playwright-level tests against a local fixture page (no live hits in CI).

## Build order
① spike + selector map → ② `internal/browser` driver + stealth → ③ `browserFlow` orchestrator (jobs, stream+poll, resume, per-step logs) → ④ `request_accounts` migration + encrypt/decrypt → ⑤ `meta` profile (signup→billing→keys) → ⑥ `--country` on `cards add` + install → ⑦ hook routes + dashboard wiring → ⑧ fixture tests → live drill on one request.

---

## Addendum A — 2026-09-27 (locked)

1. **dev.meta.ai recon — "full probe once":** one logged-in session via our extension, one test account with our email system, one OTP, single slow pass, then stop. No signup submits or OTP sends until explicitly approved. *(Superseded for target by Addendum B §1: target is omegameta.ge, not dev.meta.ai.)*
2. **Proxy:** browser-egress only. Config `browser.proxy_url` + `--proxy` per run + setup prompt; API/mail/direct stay direct. No credentials now — pluggable later. Reputable residential options (indicative, verify at signup): Bright Data (~$5–8/GB starter, account + possible KYC/minimum), Oxylabs (pay-per-GB, higher minimums, business KYC common), SOAX (flexible GB plans, lower entry minimums), IPRoyal/Decodo (budget-friendly, simpler signup). KripiCard's proxy offering stays as the known alternative.
3. **CAPTCHA/blocker recovery — all except third-party solving, configurable, default = pause for manual solve:** `abort-quarantine` (detect → screenshot → `blocked-captcha` → release card → dashboard), `pause-manual` (**default**, hold job with dashboard/CLI resume), `backoff-retry` (fresh profile/IP, capped attempts). No solvers, ever. Config `browserflow.captcha_strategy` + `--captcha-strategy` + setup prompt.

## Addendum B — 2026-09-28 (locked)

### B1. Platform correction (local correction, not a rewrite)
- The target is **omegameta.ge** — our internal employee platform (built by another team). It has **nothing to do with Meta/Facebook or dev.meta.ai** (that URL was a copy-paste error from another tab).
- `meta` as a service-profile name is therefore misleading → **rename profile `meta` → `omegameta`** everywhere it appears in future specs/code. The framework name `browserFlow` is unchanged.
- Backend bugs on omegameta are expected (another team's code, under development) — automation treats platform errors as retryable/blocked states, never as our bug to fix. Log + surface, don't chase.
- Stealth/anti-blocker/CAPTCHA work applies to **omegameta**, but keep it: local platform may still rate-limit or bot-check, and the same machinery serves future public services.

### B2. Service domain is dynamic (hard requirement)
- omegameta's domain is **not hardcoded**: dev runs on an `etc`-style host today, production domain later, other services = other domains + other flows.
- Service profile = `{name, base_url, flow steps, selectors}` — domain lives in **config per service**, never in code.
- CLI (mirror of existing `provider set-default` pattern in `cmd/onramp.go`):
  - `autokey service domain [name] [url]` — show current or set the service base URL. Reminder note for the user: **run `autokey service domain omegameta <prod-url>` when production lands.**
  - Domain lives under service config (e.g. `services.omegameta.base_url`), validated as http(s) URL at set time.
- ⚠️ NOTE — name collision: `service` top-level already owns `install/uninstall/status/logs` (systemd). The domain command nests under a service-profile subcommand (e.g. `autokey service domain ...` is ambiguous). Resolve at spec time: either `autokey services domain ...` (plural = profiles) or a new top-level group. Do NOT silently overload `service`.

### B3. Selector resilience / spec flexibility (hard requirement)
- The spec must define a **locator strategy order**, not fixed queries: `accessible-label → visible text → role → css → xpath`, first match wins; IDs/classes are last resort (they churn fastest).
- Button/field labels change less than ids/classes → profile declares **label-first selectors** with ordered fallbacks per element.
- Spec must include a **selector health check**: dry-run mode walks every profile step, reports matched/missing elements, fails fast before any signup submit. Any UI change → re-run health check, update profile YAML, never framework code.

### B4. Purpose restatement (unchanged goal, recorded)
- omegameta is for our employees; the automation exists so they stop hand-creating keys. Throughput and debuggability beat cleverness.
