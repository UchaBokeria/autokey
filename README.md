# autokey

Neon cyberpunk CLI: wildcard inbox + virtual-card pool + key pipeline in one Go binary.

```sh
go install github.com/uchabokeria/autokey@latest
autokey setup
autokey hook serve
```

## Card providers

No default provider — every card command takes an explicit `--provider`.
The config `provider:` value is a display hint only.

| provider | source | `cards create` | notes |
|---|---|---|---|
| `custom` | your own card, typed in | `cards create --provider custom` (or `cards add`) | number/expiry/CVV masked at entry, AES-256-GCM sealed with `CUSTOM_CARD_KEY`; pool-only, never minted |
| `kripi` | KripiCard API | `cards create --provider kripi --bin … --amount …` | funded virtual cards; `fund`, `details`, `freeze`, `delete` are kripi-only |
| `onramp` | Onramp Pay one-time orders | `cards create --provider onramp --product …` | crypto-funded; pay exact amount, then `onramp status --redeem-id …` |

```sh
# your own card: label + live-preview PAN entry (2s visible per group,
# then masked; last-4 echoed on submit), Luhn + brand checked
autokey cards add --label mine
# fully flagged (no prompts)
autokey cards create --provider custom --label mine \
  --number 4111111111111111 --expiry 12/28 --cvv 123
# expiry also accepts 12-28, 12.28, 1228, 12/2028; Visa/MC/Amex/
# Discover/Diners/JCB/UnionPay lengths enforced (Amex 15 + 4-digit CID)
# pool + management
autokey cards list --provider custom
autokey cards show --id custom_<rand>   # decrypts to terminal only
autokey cards remove --id custom_<rand> -y
# use it
autokey keys generate --provider custom --qty 5
```

`CUSTOM_CARD_KEY` lives in `~/.autoApiKeys/secrets.env` (0600). Without it,
custom cards can't be added or decrypted. Secrets are sealed with
PBKDF2-SHA256 (210k rounds) + AES-256-GCM; the DB holds ciphertext only.

## Hook

```sh
curl -X POST http://127.0.0.1:8765/v1/generate \
  -H "Authorization: Bearer $HOOK_BEARER" \
  -d '{"keyQuantity": 5, "provider": "custom"}'
```

## Persistent ingress

`autokey setup` enables the service by default (confirm prompt).
`autokey service install` enables two user units:

- `autokey.service` — the hook (`Restart=always`).
- `autokey-tunnel.service` — ingress, `BindsTo` the hook:
  - **Named tunnel** if `~/.config/autokey/tunnel.yml` exists
    (stable hostname, e.g. `https://hook.uchabokeria.space` — create
    once via `cloudflared tunnel create` + dashboard hostname route).
  - **Quick tunnel fallback** otherwise: fresh `trycloudflare.com`
    URL per restart, inbox Worker auto-repointed via `worker deploy`.

Both survive reboots (linger + `WantedBy=default.target`).
Turn it off with `autokey service uninstall`; back on with
`autokey service install`.

## Reading one-time codes

```sh
# wait up to 2m (default) for a fresh code to an email user
autokey inbox otp --email jun01032026-a3f9@my.com
# longer wait, or just print the newest stored code
autokey inbox otp --email jun01032026-a3f9@my.com --timeout 5m
autokey inbox otp --email jun01032026-a3f9@my.com --latest
# machine-readable
autokey inbox otp --email jun01032026-a3f9@my.com --json
```

Codes are extracted from subject + text + HTML with OTP-context
scoring (4–8 digits near verification wording); only mail received
after the wait starts counts, so stale codes never match.

## Web dashboard

```sh
autokey dashboard                  # :8766 by default (hook.port+1)
autokey dashboard --port 9000
```

Same bearer as the hook (`HOOK_BEARER`, header or `?token=`).
Tabs: Overview · Cards (per-service stats, custom remove) · Keys
(requests + key drill-down) · Inbox (filter, full message) · OTP
lookup · Onramp stock · Logs. Single binary: the SPA is embedded
via `go:embed`; rebuild it with `make web` (needs node).

## One-shot commands

Every CLI command maps 1:1 to one function — nothing hidden:

| command | function |
|---|---|
| `email mint` | `pool.CreateUniqueEmailUser` (+ reserve row) |
| `keys generate` | `flow.GenerateDetails` |
| `cards list` | `pool.ListPool` |
| `cards create` | provider `Mint` + `pool.Register` |
| `cards remote` | kripi `List` (account side) |
| `cards add/remove/show` | custom `Add` / delete / `Secrets` |
| `cards fund/details/freeze/delete` | kripi `Fund/Details/Freeze/Delete` |
| `onramp stock/status` | `Stock` / `CheckStatus` |
| `inbox list/otp` | queries + `mail.LatestOTP` / `WaitForOTP` |
| `inbox watch` | `mail.PollOnce` loop |
| `worker deploy/status` | upload + DNS + catch-all / read back |
| `provider list/set-default` | registry + config hint |
| `hook serve` | `hook.Server.Serve` |
| `dashboard` | `dashboard.Server.Serve` |
| `service install/uninstall/status/logs` | unit files + systemctl |
| `browser install` | playwright driver download (one-time) |
| `services domain [name] [url]` | show/set service base URL |

## Browser automation (#1)

Per-request headless runs: signup (password-default, OTP fallback) →
billing → keys. Service profiles carry their own base URL:

```sh
autokey browser install            # one-time playwright driver
autokey services domain omegameta https://host   # dev host now, prod later
autokey cards add --label mine --country US       # billing country, default US
autokey keys generate --provider custom --qty 1 \
  --proxy http://user:pass@host:port \            # optional residential egress
  --captcha-strategy pause-manual                 # or abort-quarantine|backoff-retry
```

Long runs: `POST /v1/generate {"async":true}` → `202 {request_id}`,
poll `GET /v1/requests/{id}`, or stream `{"stream":true}` (SSE step
events). Per-step logs land in `request_steps` (dashboard Keys tab);
account passwords are AES-256-GCM sealed per request. CAPTCHAs are
never solved — pause (default), abort-quarantine, or backoff-retry.

