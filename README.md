# Easee OCPP Proxy

A local-network OCPP 1.6J proxy for Easee One chargepoints. It either auto-authorises
charge sessions locally, or transparently proxies **one** chargepoint to a remote OCPP
Central System (CSMS) over an authenticated, encrypted (`wss://`) link — while accepting
plain `ws://` from the Easee side (no certificate upload required).

See [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md) and
[docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md) for the full design.

## Status

**Milestones 1–3 complete.**

Scheduling & aliases (FR-41/FR-42):

- Named charging schedules (start/stop, local wall-clock **DST-aware**, may cross
  midnight), defined centrally and assigned per chargepoint. Outside the window the
  proxy won't auto-start; it starts when the window opens (car plugged in) and stops
  (RemoteStopTransaction) when it closes. Optional IANA timezone override.
- Per-chargepoint friendly **alias** (e.g. "Garage Left") shown on the dashboard.

Milestone 3 (admin dashboard):

- Authenticated `/admin` UI (username + bcrypt password, session cookie) with a
  first-run setup page when no password is set.
- Dashboard home page: live per-chargepoint state (connection, connector status,
  session, energy/power) with a 5s auto-refresh, plus one-click selection of the
  remotely-managed chargepoint.
- Settings pages: add/remove chargepoints, remote server + BootNotification
  anonymisation, and admin credentials. Changes persist to the config file.

Earlier milestones:

Milestone 1 (skeleton):

- Single-port HTTP + WebSocket server (`ws://` OCPP and `http://` `/admin` on one port).
- Chargepoint allow-list enforcement and `ocpp1.6` subprotocol negotiation.
- OCPP-J frame parsing/building.
- Config load, validation, and atomic save.
- `/admin` route reserved (placeholder page).

Milestone 2 (local auto-authorisation):

- Local Central System: auto-accepts BootNotification, Heartbeat, Authorize,
  Start/StopTransaction (with generated transaction ids), StatusNotification,
  MeterValues, DataTransfer; unknown actions get `CALLERROR NotSupported`.
- Live per-chargepoint state (connection, connector status, session, energy/power),
  with energy normalised from the reported unit (kWh/Wh).
- Downstream liveness watchdog: closes a connection silent for 2× the heartbeat
  interval, using an injectable clock for deterministic tests.

Not yet implemented: admin dashboard (M3), upstream proxying + BootNotification
anonymisation (M4), lifecycle cross-linking (M5), telemetry polish (M6). Proxied
chargepoints are observe-only until M4.

## Prerequisites

- **Go 1.22+** — not currently installed on the dev machine. Install from
  <https://go.dev/dl/> (or `winget install GoLang.Go` on Windows), then reopen the shell.

## Build & run

```bash
go mod tidy          # fetches github.com/coder/websocket and gopkg.in/yaml.v3
go build ./...
cp config.example.yaml config.yaml   # then edit
./easee-proxy -config config.yaml
```

The proxy logs to stderr (structured). Point an allow-listed Easee at
`ws://<proxy-host>:9000/<chargepoint-id>`; open `http://<proxy-host>:9000/admin` for the
(placeholder) admin UI.

## Test

```bash
go test ./...
```

## Layout

```
cmd/proxy         entry point
internal/config   config load / validate / atomic save
internal/ocpp     OCPP-J framing
internal/manager  shared state: config, allow-list, role classification
internal/server   single-port HTTP + WebSocket router
internal/session  downstream connection handler (observe-only in M1)
internal/admin    /admin handler (placeholder in M1)
deploy/           systemd unit
docs/             requirements + implementation plan
```
