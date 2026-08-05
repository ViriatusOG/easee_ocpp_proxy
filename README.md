# Easee OCPP Proxy

A small, self-hosted proxy that sits on your local network between your EV chargepoints
and (optionally) a remote OCPP Central System (CSMS). For each chargepoint it either
**auto-authorises charging locally** or **transparently proxies one chargepoint** to a
remote CSMS over an authenticated, encrypted link — while the chargers themselves only
ever speak plain, unencrypted OCPP on your LAN.

It's built and tested against the **Easee One**, but it speaks standard **OCPP 1.6J**, so
it should work with **any charger brand that supports local OCPP 1.6** (the Easee-specific
conveniences below simply become no-ops for chargers that don't need them).

> Full design rationale lives in [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md) and
> [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md).

## Why — problems it solves

- **No certificate wrangling.** A charger's native *secure* OCPP requires uploading a TLS
  certificate into the unit, which is fiddly and some chargers' certificate validation is
  notoriously picky. The proxy terminates TLS itself: the charger connects over plain
  `ws://` on the trusted LAN, and the proxy makes the encrypted, authenticated `wss://`
  connection upstream.
- **Charge-point ID length limits.** Some chargers cap the OCPP charge point identity at
  25 characters, while many remote servers issue longer identities (e.g. 36-character
  UUIDs). The proxy maps a short local ID to the longer upstream ID at the connection
  layer, so both sides are happy.
- **Manufacturer disclosure.** The only place OCPP reveals the manufacturer is the
  `BootNotification`. The proxy can rewrite the vendor / model / firmware / serial to
  values you configure before forwarding it to the remote server.
- **Provisioning on reconnect.** When you switch a charger to remote management it
  reconnects *without rebooting*, so a remote server would never re-run its post-boot
  configuration and could mis-account sessions. The proxy asks the charger to re-present
  its boot on every upstream connect, so the remote server always provisions and tracks
  the session correctly.
- **Scheduling when the built-in scheduler is disabled.** Putting a charger onto OCPP
  typically disables its own charging schedules. The proxy provides its own DST-aware
  charging windows for locally-managed chargers.
- **One box, mixed roles.** Run several chargers with local auto-authorisation while
  handing exactly one to a remote server — and switch which one, at runtime, from a web
  dashboard, a phone, or Home Assistant.

## Features

- **Accepts multiple chargers** over plain `ws://` on a single port, gated by an
  allow-list of chargepoint IDs, with `ocpp1.6` subprotocol negotiation.
- **Local auto-authorisation** — the proxy acts as the Central System, auto-accepting the
  charge session. Optional **auto-start** (`RemoteStartTransaction`) for chargers that
  wait for backend authorisation before charging.
- **Upstream proxying** of one chargepoint to a remote CSMS: `wss://` with HTTP Basic
  auth, transparent bidirectional relay (including remote-initiated commands), short→long
  ID mapping, BootNotification anonymisation, and forced re-provisioning on reconnect.
- **DST-aware charging schedules** — named windows (`HH:MM`, may cross midnight), defined
  centrally and assigned per chargepoint, evaluated in a configurable/local timezone.
  Per-chargepoint **always-on ⇄ scheduled** toggle that keeps the schedule assigned.
- **Synchronised charging** — a second charger can *mirror* the proxied charger's
  start/stop, so two vehicles share the remote server's smart-charging windows through one
  remote identity. Its energy is never reported upstream; only the proxied charger's is.
- **Friendly aliases** (e.g. "Garage Left") shown across the UI.
- **Admin dashboard** — mobile-responsive, live in-place refresh (no full-page reloads),
  persistent sign-in (survives restarts), one-tap role switching, and settings for
  chargepoints, schedules, the remote server, and credentials. Config changes persist.
- **Robust connection handling** — per-connection isolation, liveness watchdogs (closes a
  link that goes silent for 2× the heartbeat interval, timed independently per direction
  for a proxied link), and clean teardown when either side drops.
- **JSON API + Home Assistant integration** — a token-authenticated API exposes state and
  role control; the bundled custom integration surfaces each chargepoint as a Home
  Assistant device (see [homeassistant/](homeassistant/)).
- **Single static binary**, structured logging, systemd unit.

## How it works

```
   charger #1 ──ws (ocpp1.6)──┐
   charger #2 ──ws (ocpp1.6)──┤   ┌──────────────┐   wss (ocpp1.6 + auth)
   charger #N ──ws (ocpp1.6)──┼──▶│  easee-proxy │ ─────────────────────▶  remote CSMS
                              │   │   :PORT      │        (proxied charger only)
                              │   │  /     OCPP  │
                              │   │  /admin  UI  │
                              │   │  /api    JSON│
                              └──▶└──────────────┘
```

The chargers connect over plain `ws://`. Non-proxied chargers are served locally; the one
proxied charger is relayed to the remote CSMS over `wss://`. The admin UI and JSON API are
served on the same port.

## Prerequisites

- **Go 1.22 or newer** to build (see `go.mod` for the exact toolchain version).
  Install from <https://go.dev/dl/> (or `winget install GoLang.Go` on Windows).
- A host to run it on (Linux recommended; any OS Go targets works).
- Chargers that can be pointed at a local OCPP 1.6J URL (plain `ws://`).

## Build

```bash
go build -o easee-proxy ./cmd/proxy
```

This fetches the few dependencies and produces a single static binary.

## Run

```bash
cp config.example.yaml config.yaml   # then edit to taste
./easee-proxy -config config.yaml
```

Then:

1. Open `http://<host>:9000/admin` and complete the **first-run setup** (create an admin
   password).
2. On the **Chargepoints** page, add each charger's OCPP identity (the ID it presents in
   its OCPP URL). Optionally give it an alias.
3. Point each charger's OCPP endpoint at `ws://<host>:9000/<chargepoint-id>` (plain
   `ws://`, subprotocol `ocpp1.6`, no TLS, no auth).
4. To hand one charger to a remote server: configure it on the **Remote server** page,
   then pick that charger's role as **proxied** on the dashboard.

The `-debug` flag adds verbose per-frame logging for troubleshooting.

## Install (Linux / systemd)

```bash
sudo install -m 0755 easee-proxy /usr/local/bin/easee-proxy
sudo useradd --system --no-create-home easee-proxy
sudo mkdir -p /etc/easee-proxy && sudo cp config.example.yaml /etc/easee-proxy/config.yaml
sudo cp deploy/easee-proxy.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now easee-proxy
```

The unit runs as a dedicated non-root user and keeps `config.yaml` writable (admin changes
are persisted back to it). The admin UI and API are plain HTTP on the shared port — keep
the service on a trusted LAN and don't expose the port to the internet.

## Configuration

Everything is in one YAML file (`config.example.yaml` is documented). Most settings are
also editable from the admin UI, which writes changes back atomically. Highlights:

- `chargepoints` — allow-list of charger IDs; `aliases` — friendly names.
- `proxied_id` — which single charger (if any) is proxied.
- `remote` — the upstream CSMS URL, upstream ID, credentials.
- `boot_anonymise` — the vendor/model/etc. presented upstream.
- `proxy_force_boot` — trigger a `BootNotification` on every upstream connect so the CSMS
  re-provisions (default `true`); disable once the CSMS has configured the charger.
- `proxy_normalise_meter` — when rotating multiple chargers through one remote identity,
  present the CSMS a single monotonic meter so its lifetime register never jumps/rolls
  back on a unit switch (default `false`; per-session energy is unchanged).
- `schedules` / `device_schedules` / `timezone` — charging windows.
- `local_auto_start` — auto-start behaviour for local chargers.
- `api_token` — enables the JSON API (manage it from Admin → Account).

## Home Assistant

A custom integration under [homeassistant/custom_components/easee_ocpp_proxy/](homeassistant/custom_components/easee_ocpp_proxy/)
exposes each chargepoint as a Home Assistant device with sensors (status, power, session
energy, session, schedule), binary sensors (online, charging), and a **role** selector
(view + role control only, no admin functions). See [homeassistant/README.md](homeassistant/README.md).

## Test

```bash
go test ./...
```

## Project layout

```
cmd/proxy                       entry point
internal/config                 config load / validate / atomic save
internal/ocpp                   OCPP-J framing + message types
internal/manager                shared state: config, allow-list, roles, schedules, sessions
internal/schedule               charging-window evaluation (DST-aware)
internal/clock                  injectable clock for deterministic liveness tests
internal/state                  live per-chargepoint state
internal/session                downstream handler: local auto-authorise + proxy relay
internal/upstream               dials the remote CSMS
internal/server                 single-port HTTP + WebSocket router
internal/admin                  /admin web UI
internal/api                    /api JSON API
deploy/                         systemd unit
homeassistant/                  Home Assistant custom integration
docs/                           requirements + implementation plan
```
