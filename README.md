[English](/README.md) | [فارسی](/README.fa_IR.md) | [العربية](/README.ar_EG.md) | [中文](/README.zh_CN.md) | [Español](/README.es_ES.md) | [Русский](/README.ru_RU.md) | [Türkçe](/README.tr_TR.md)

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./media/3x-ui-dark.png">
    <img alt="3x-ui" src="./media/3x-ui-light.png">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/DarkPoesidon/3x-ui/releases"><img src="https://img.shields.io/github/v/release/DarkPoesidon/3x-ui" alt="Release"></a>
  <a href="https://github.com/DarkPoesidon/3x-ui/actions"><img src="https://img.shields.io/github/actions/workflow/status/DarkPoesidon/3x-ui/release.yml.svg" alt="Build"></a>
  <a href="#"><img src="https://img.shields.io/github/go-mod/go-version/DarkPoesidon/3x-ui.svg" alt="GO Version"></a>
  <a href="https://github.com/DarkPoesidon/3x-ui/releases/latest"><img src="https://img.shields.io/github/downloads/DarkPoesidon/3x-ui/total.svg" alt="Downloads"></a>
  <a href="https://www.gnu.org/licenses/gpl-3.0.en.html"><img src="https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true" alt="License"></a>
</p>

A web control panel for [Xray-core](https://github.com/XTLS/Xray-core) servers — deploy, configure and monitor a wide range of proxy protocols from a single VPS or across many nodes.

This repository is a fork of [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui). It exists for one reason: **first-class AnyTLS support**, with per-client passwords, quotas, expiry, share links and subscriptions — the same treatment every other protocol gets. Everything else from upstream is carried forward and kept in sync.

> [!IMPORTANT]
> This project is intended for personal use only. Please do not use it for illegal purposes or in a production environment.

## AnyTLS

Xray-core does not speak AnyTLS, so the panel runs one `anytls-server` sidecar process per AnyTLS inbound, next to the Xray binary, and drives it over a management API:

- **Per-client identity** — passwords, traffic quotas, expiry dates and IP limits, accounted exactly like a VLESS or Trojan client.
- **Share links, QR codes and subscriptions** — AnyTLS proxies are emitted into Clash subscriptions alongside the rest.
- **Sub-nodes** — AnyTLS inbounds run on remote nodes, not just the panel host.
- **Self-healing** — sidecars are reconciled on config change and restart; processes orphaned by a previous run are reaped at startup.

> [!WARNING]
> The sidecar must be the **patched** build from [DarkPoesidon/anytls-rs](https://github.com/DarkPoesidon/anytls-rs), which exposes the management API this panel drives. Upstream `ssrlive/anytls-rs` has no such API and leaves every AnyTLS inbound unusable. Release tarballs and the Docker image already bundle the correct binary; set `ANYTLS_REPO` only if you host your own build.

## Features

- **Multi-protocol inbounds** — VLESS, VMess, Trojan, Shadowsocks, AnyTLS, WireGuard, AmneziaWG, Hysteria2, MTProto, HTTP, SOCKS (Mixed) and Dokodemo-door / Tunnel.
- **Modern transports & security** — TCP (Raw), mKCP, WebSocket, gRPC, HTTPUpgrade and XHTTP, secured with TLS, XTLS and REALITY.
- **Fallbacks** — serve multiple protocols on a single port (e.g. VLESS and Trojan on 443) via Xray's fallback support.
- **Per-client management** — traffic quotas, expiry dates, IP limits, live online status, one-click share links, QR codes and subscriptions.
- **Traffic statistics** — per inbound, per client and per outbound, with reset controls.
- **Multi-node support** — manage and scale across multiple servers from a single panel.
- **Outbound & routing** — WARP, NordVPN, custom routing rules, load balancers and outbound proxy chaining.
- **Built-in subscription server** with multiple output formats and [custom page templates](docs/custom-subscription-templates.md).
- **Telegram bot** for remote monitoring and management.
- **RESTful API** with in-panel Swagger documentation.
- **Flexible storage** — SQLite (default) or PostgreSQL.
- **13 UI languages** with dark and light themes.
- **Fail2ban integration** for enforcing per-client IP limits.

## Screenshots

<details>
<summary>Click to expand</summary>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./media/01-overview-dark.png">
  <img alt="Overview" src="./media/01-overview-light.png">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./media/02-add-inbound-dark.png">
  <img alt="Inbounds" src="./media/02-add-inbound-light.png">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./media/03-add-client-dark.png">
  <img alt="Add client" src="./media/03-add-client-light.png">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./media/05-add-nodes-dark.png">
  <img alt="Nodes" src="./media/05-add-nodes-light.png">
</picture>

</details>

## Quick start

```bash
bash <(curl -Ls https://raw.githubusercontent.com/DarkPoesidon/3x-ui/main/install.sh)
```

To install a specific version, append its tag (e.g. `v3.7.3`):

```bash
bash <(curl -Ls https://raw.githubusercontent.com/DarkPoesidon/3x-ui/main/install.sh) v3.7.3
```

To install the rolling **dev** build (latest per-commit pre-release from `main`, not a stable release), pass `dev-latest`:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/DarkPoesidon/3x-ui/main/install.sh) dev-latest
```

The installer generates a random username, password and access path, then prints the panel URL. It also writes them to `/etc/x-ui/install-result.env` (root-only), so the full link survives a lost terminal:

```bash
cat /etc/x-ui/install-result.env
```

The URL scheme reflects what the panel actually serves: if certificate setup was skipped or failed, you get an `http://` link rather than an `https://` one the browser cannot open. The installer also checks that the panel is listening and offers to open its port in `ufw` or `firewalld` — your cloud provider's own firewall still needs the port opened by hand.

Run `x-ui` afterwards for the management menu: start/stop, view or reset credentials, manage SSL certificates and more.

### Unattended install

The installer also runs **non-interactively** for cloud-init. Set `XUI_NONINTERACTIVE=1` (or pipe it with no TTY) and it installs end to end with zero prompts, generating random credentials and writing them to `/etc/x-ui/install-result.env`. See [`deploy/`](deploy/) for:

- [Cloud-init user-data](deploy/cloud-init/) — unattended install on any cloud (Hetzner / AWS / DO / Vultr / GCP / Azure / Oracle)
- [Hetzner Cloud notes](deploy/marketplace/hetzner/) — cloud-init deployment on Hetzner

## Supported platforms

**Operating systems:** Ubuntu, Debian, Armbian, Fedora, CentOS, RHEL, AlmaLinux, Rocky Linux, Oracle Linux, Amazon Linux, Virtuozzo, Arch, Manjaro, Parch, openSUSE (Tumbleweed / Leap), Alpine and Windows.

**Architectures:** `amd64` · `386` · `arm64` (aarch64) · `armv7` · `armv6` · `armv5` · `s390x`.

## Docker

`docker compose up -d` builds the image from this repository and keeps using SQLite. To run with the bundled PostgreSQL service, uncomment the two `XUI_DB_*` env lines in `docker-compose.yml` and start with the profile:

```bash
docker compose --profile postgres up -d
```

Tagged releases also publish an image to `ghcr.io/darkpoesidon/3x-ui`.

The image bundles Fail2ban (enabled by default) to enforce per-client **IP limits**. Fail2ban bans offenders with `iptables`, which requires the `NET_ADMIN` capability. `docker-compose.yml` already grants it via `cap_add`; if you start the container with `docker run` instead, add the capabilities yourself, otherwise bans are logged but never applied:

```bash
docker run -d --cap-add=NET_ADMIN --cap-add=NET_RAW ... ghcr.io/darkpoesidon/3x-ui
```

`ANYTLS_REPO` is a build argument as well as a runtime variable, so a self-hosted `anytls-rs` fork can be baked in at build time:

```bash
docker build --build-arg ANYTLS_REPO=your-org/anytls-rs -t 3x-ui .
```

## Database options

Two backends, chosen during install:

- **SQLite** (default) — a single file at `/etc/x-ui/x-ui.db`. Zero setup, ideal for small and medium deployments.
- **PostgreSQL** — recommended for high client counts or multi-node setups. The installer can install PostgreSQL locally for you, or accept a DSN for an existing server.

At runtime the backend is selected via environment variables (the installer writes these to `/etc/default/x-ui` for you):

```
XUI_DB_TYPE=postgres
XUI_DB_DSN=postgres://xui:password@127.0.0.1:5432/xui?sslmode=disable
```

### Migrating an existing SQLite install to PostgreSQL

```bash
x-ui migrate-db --dsn "postgres://xui:password@127.0.0.1:5432/xui?sslmode=disable"
# then set XUI_DB_TYPE and XUI_DB_DSN in /etc/default/x-ui and restart:
systemctl restart x-ui
```

The source SQLite file is left untouched; remove it manually once you have verified the new backend.

## Environment variables

| Variable | Description | Default |
| --- | --- | --- |
| `XUI_DB_TYPE` | Database backend: `sqlite` or `postgres` | `sqlite` |
| `XUI_DB_DSN` | PostgreSQL connection string (when `XUI_DB_TYPE=postgres`) | — |
| `XUI_DB_FOLDER` | Directory for the SQLite database file | `/etc/x-ui` |
| `XUI_DB_MAX_OPEN_CONNS` | Maximum open connections (PostgreSQL pool) | — |
| `XUI_DB_MAX_IDLE_CONNS` | Maximum idle connections (PostgreSQL pool) | — |
| `XUI_INIT_WEB_BASE_PATH` | The initial URI path for the web panel | `/` |
| `XUI_ENABLE_FAIL2BAN` | Enable Fail2ban-based IP-limit enforcement | `true` |
| `XUI_LOG_LEVEL` | Log verbosity (`debug`, `info`, `warning`, `error`) | `info` |
| `XUI_DEBUG` | Enable debug mode | `false` |
| `XUI_TUNNEL_HEALTH_MONITOR` | Enable the tunnel health monitor (probes a URL and restarts xray after repeated failures; a restart drops all clients) | `false` |
| `XUI_TUNNEL_HEALTH_PROXY` | Proxy the probe is sent through; point it at a local xray inbound so the probe tests the tunnel (e.g. `socks5://127.0.0.1:1080`). Empty means the probe only checks host connectivity | — |
| `XUI_TUNNEL_HEALTH_URL` | URL probed for tunnel health | `https://www.cloudflare.com/cdn-cgi/trace` |
| `XUI_TUNNEL_HEALTH_INTERVAL` | Interval between probes | `30s` |
| `XUI_TUNNEL_HEALTH_TIMEOUT` | Per-probe timeout | `10s` |
| `XUI_TUNNEL_HEALTH_FAILURES` | Consecutive failures before a restart is triggered | `3` |
| `XUI_TUNNEL_HEALTH_COOLDOWN` | Minimum delay between consecutive restarts | `5m` |
| `ANYTLS_REPO` | GitHub repository the `anytls-server` sidecar is fetched from | `DarkPoesidon/anytls-rs` |

Installer-only variables (`XUI_NONINTERACTIVE`, `XUI_USERNAME`, `XUI_PASSWORD`, `XUI_PANEL_PORT`, `XUI_WEB_BASE_PATH`, `XUI_SSL_MODE`, `XUI_DOMAIN`, `XUI_OPEN_FIREWALL`, …) are documented in [`.env.example`](.env.example).

## Documentation

The docs live in this repository:

- [Architecture overview](docs/architecture.md)
- [Engineering guide](docs/engineering-guide.md)
- [Custom subscription templates](docs/custom-subscription-templates.md)
- [Real client IP behind a proxy](docs/real-client-ip.md)
- [Xray DNS configuration](docs/xray-dns.md)

## Supported languages

The panel UI is available in 13 languages:

English · فارسی · العربية · 中文（简体） · 中文（繁體） · Español · Русский · Українська · Türkçe · Tiếng Việt · 日本語 · Bahasa Indonesia · Português (Brasil)

## Contributing

Contributions are welcome. Please read the [Contributing Guide](/CONTRIBUTING.md) before opening an issue or pull request.

## Credits

This fork stands on other people's work:

- [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) — the upstream panel this fork tracks (**GPL-3.0**)
- [alireza0/x-ui](https://github.com/alireza0/x-ui) — an earlier fork much of upstream grew from
- [XTLS/Xray-core](https://github.com/XTLS/Xray-core) — the proxy core the panel manages
- [anytls/anytls-go](https://github.com/anytls/anytls-go) and [ssrlive/anytls-rs](https://github.com/ssrlive/anytls-rs) — the AnyTLS protocol and the Rust implementation our sidecar is patched from
- [Iran v2ray rules](https://github.com/chocolate4u/Iran-v2ray-rules) (**GPL-3.0**) — routing rules with built-in Iranian domains, security and adblocking
- [Russia v2ray rules](https://github.com/runetfreedom/russia-v2ray-rules-dat) (**GPL-3.0**) — automatically updated routing rules for domains blocked in Russia

## Community tools

Built by the community around 3x-ui:

- [terraform-provider-3x-ui](https://github.com/batonogov/terraform-provider-threexui) (**MIT**) — manage inbounds, clients, panel settings and Xray configuration as code with Terraform / OpenTofu

## License

Released under the [GNU General Public License v3.0](/LICENSE), the same licence as upstream 3x-ui.
