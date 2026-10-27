# nginx-3x-ui-subscription-proxy

[Русская версия](README_RU.md)

Merges subscriptions from several [3x-UI](https://github.com/MHSanaei/3x-ui) servers (and optionally third-party VPN services) into a single link. Useful when you run servers in different locations but want clients to add just one subscription.

It is a single Go service in a ~10 MB distroless image: it fetches all sources in parallel, serves `routing.json`, and in standalone mode obtains and renews its own Let's Encrypt certificate. No nginx, certbot or cron.

## Important

1. A client must have the same **subscription ID** on all your 3x-UI servers.
2. Subscription encryption must be enabled on all 3x-UI servers.

## Quick start

```bash
git clone https://github.com/normalisht/nginx-3x-ui-subscription-proxy.git
cd nginx-3x-ui-subscription-proxy
make env    # creates .env from .env.example — fill it in
make up     # build, start, wait for the healthcheck
```

The subscription link is `https://<SITE_HOST>/<SUB>/<subscription_ID>`, e.g. `https://sub.example.com/sub/abc123`.

## Configuration (`.env`)

| Variable | Description |
|---|---|
| `SERVERS` | 3x-UI subscription URLs, space-separated. The client's ID is appended to each: `https://s1.com/sub/` → `https://s1.com/sub/<id>` |
| `EXTERNAL_SUBSCRIPTIONS` | Optional. Full third-party subscription URLs, space-separated, fetched as-is |
| `EXTERNAL_SUBSCRIPTION_HEADERS` | Optional. Headers sent to `EXTERNAL_SUBSCRIPTIONS` to mimic a regular VPN client: `Key: Value`, separated by `;;` or newlines, e.g. `User-Agent: Happ/2.4.1 (Android 13; SDK 33);;Hwid: 1234567890abcdef` |
| `EXTERNAL_SUBSCRIPTIONS_JSON` | `1` — third-party subscriptions return JSON configs; they are merged into one (arrays concatenated, objects merged, duplicate `remarks` numbered). `SERVERS` are not used in this mode |
| `SUB` | Static part of the subscription path, default `sub` |
| `SITE_HOST` | Service domain: the certificate is issued for it, and the `routing.json` link points to it |
| `ROUTING_ENABLED` | `1` — prepend links to `https://<SITE_HOST>/routing/routing.json` (routing rules for Happ and similar clients) |
| `EMAIL` | Optional. Let's Encrypt account email (standalone mode) |
| `COMPOSE_FILE` | `compose.edge.yaml` — run behind a shared reverse proxy (see below) |

## Modes

**Standalone** (`compose.yaml`, default): the service listens on ports 80 and 443 and gets a Let's Encrypt certificate for `SITE_HOST` on the first HTTPS request (TLS-ALPN-01 or HTTP-01). Certificates are kept in the `certs` volume. Port 80 redirects to HTTPS.

**Behind a shared reverse proxy** (`compose.edge.yaml`): plain HTTP on `subproxy:8080` in the external Docker network `proxy`, no published ports. TLS is the proxy's job — see [edge-proxy](https://github.com/normalisht/edge-proxy). Set `COMPOSE_FILE=compose.edge.yaml` in `.env`.

## Endpoints

| Path | Response |
|---|---|
| `/<SUB>/<id>` | merged subscription in base64; `502` if no source returned anything |
| `/routing/routing.json` | routing rules from `routing.json` (mounted — edit and `make restart`) |
| `/healthz` | `ok` |

Sources are fetched in parallel with a 10-second timeout each; an unavailable source is skipped and logged (host only — subscription IDs are not logged). TLS verification of sources is disabled on purpose: 3x-UI panels often use self-signed certificates.

## Commands

`make help` lists everything: `up`, `down`, `restart` (apply `.env`/`routing.json` changes), `update` (`git pull` + restart), `status`, `logs`, `check` (gofmt, vet, tests).

## License

MIT, see `LICENSE`.
