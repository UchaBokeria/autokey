# Blackwall deployment (24/7 host)

Production ingress runs on blackwall (`13.140.155.113`, Ubuntu 24.04),
not on the dev machine. Same user/home layout (`scriptkid`,
`/home/scriptkid`), systemd user units, linger on.

## What's on blackwall

| piece | location |
|---|---|
| binary | `~/go/bin/autokey` (static, `CGO_ENABLED=0 go build`) |
| home | `~/.autoApiKeys/` (config.yaml, secrets.env 0600, autokey.db) |
| tunnel creds | `~/.cloudflared/c81e46f8-….json` (0400) |
| tunnel config | `~/.config/autokey/tunnel.yml` (named `autokey-hook`) |
| units | `~/.config/systemd/user/autokey{,-tunnel}.service` (enabled) |
| cloudflared | `~/.local/bin/cloudflared` |
| public URL | `https://hook.uchabokeria.space` (dashboard hostname route) |

## Redeploy

```sh
CGO_ENABLED=0 go build -o /tmp/opencode/autokey-linux .
scp /tmp/opencode/autokey-linux 13.140.155.113:/home/scriptkid/go/bin/autokey
ssh 13.140.155.113 'systemctl --user restart autokey.service'
```

Config/secrets/DB changes: copy files, keep 0600, restart hook.
Worker inbox URL only changes if the public hostname changes
(`autokey worker deploy --inbox-url …`).

## Cutover notes

- Stop the local tunnel before starting the remote one; both serve
  the same hostname (Cloudflare load-balances, so overlap is safe
  but confusing).
- Local units are disabled (`systemctl --user disable --now
  autokey.service autokey-tunnel.service`); blackwall owns ingress.
- Tunnel `autokey-hook` is remotely managed (migrated in dashboard);
  hostname routes are edited in Zero Trust, not in `tunnel.yml`.
