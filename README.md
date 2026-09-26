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
