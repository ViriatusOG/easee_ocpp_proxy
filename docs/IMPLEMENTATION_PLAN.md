# Easee OCPP Proxy — Implementation Plan

**Status:** Draft v0.2 (aligned with capture findings — Appendix A of REQUIREMENTS)
**Date:** 2026-08-02
**Companion to:** [REQUIREMENTS.md](REQUIREMENTS.md) (FR-/D-/NFR- references below point there)

---

## 1. Overview

A single Go binary that:

- Serves, on **one TCP port**, both the plaintext OCPP 1.6J WebSocket endpoint
  (Easee-facing, `ws://`) and a plain-HTTP admin UI at `/admin` (D-1, D-2).
- Auto-authorises all non-proxied CPs by acting as their Central System (§5.3).
- Relays exactly one designated CP to a remote CSMS over `wss://` with Basic auth (D-3),
  mapping a short Easee CP ID to a longer upstream CP ID (D-7) and anonymising the vendor
  fields of `BootNotification` before forwarding it upstream (D-6).
- Tracks live per-CP state from the OCPP stream and presents a dashboard-first admin UI
  (§5.6 IA, §5.8).

Design principles: **per-connection isolation** (one fault never touches another CP),
**context-based cancellation** so closing one side cleanly tears down the other, and
**passive observation** for telemetry (never inject OCPP traffic — FR-38).

---

## 2. Technology & Dependencies

- **Language:** Go 1.22+ (uses `net/http.ServeMux` method/pattern routing; `log/slog`).
- **WebSocket:** `github.com/coder/websocket` (context-aware read/write, clean close
  semantics; formerly `nhooyr.io/websocket`). Gorilla is an acceptable fallback.
- **Config (YAML):** `gopkg.in/yaml.v3` (D-5, Q13 default).
- **Password hashing:** `golang.org/x/crypto/bcrypt` (D-4, FR-31).
- **HTML templates:** stdlib `html/template` (D-5 — server-rendered, no JS framework).
- **Logging:** stdlib `log/slog` (structured, NFR-4).
- No web framework, no ORM, no external DB — state is in-memory (FR-39).

Rationale recap (NFR-1): static binary, trivial systemd deployment, strong concurrency
primitives for long-lived connections and their timers.

---

## 3. Project Layout

```
easee_ocpp_proxy/
├── cmd/
│   └── proxy/
│       └── main.go                # flag parsing, wiring, graceful shutdown
├── internal/
│   ├── config/                    # Config struct, YAML load, atomic save, validation
│   ├── ocpp/                      # OCPP-J framing + message types + helpers
│   ├── manager/                   # central registry: config, sessions, CP state, role switching
│   ├── session/                   # LocalSession + ProxySession, liveness timers
│   ├── upstream/                  # CSMS dialer (wss + Basic auth + TLS)
│   ├── state/                     # per-CP live telemetry model + observers
│   └── admin/                     # HTTP handlers, auth, templates
├── web/
│   └── templates/                 # dashboard.html, chargepoints.html, remote.html, login.html
├── docs/
│   ├── REQUIREMENTS.md
│   └── IMPLEMENTATION_PLAN.md
├── deploy/
│   └── easee-proxy.service        # systemd unit
├── config.example.yaml
├── go.mod
└── README.md
```

Module path (placeholder): `github.com/ipeel/easee-ocpp-proxy`.

---

## 4. Configuration Model

```yaml
# config.yaml
listen_addr: ":9000"            # single port for OCPP ws:// and http:// /admin
heartbeat_interval_s: 300       # advertised to CPs; idle liveness cutoff = 2× this, both links (FR-24/25)
upstream_timeout_s: 30          # initial upstream dial/connect timeout only (FR-17)

chargepoints:                   # allow-list of downstream Easee CP IDs (≤25 chars, FR-3/FR-5a)
  - K9PZQ4RT                     # Easee connects to ws://<proxy>/K9PZQ4RT (root path, D-8)
  - M4XTR7Q2

proxied_id: K9PZQ4RT            # the single remotely-managed CP, or "" for none (FR-6/FR-7)

remote:                         # upstream CSMS (used only when a CP is proxied)
  url: wss://csms.example.com             # base: scheme + host [+ optional path prefix] (D-8)
  upstream_id: a3f71c92-6b48-4e05-9d1f-2c7a5e0b93d4   # 36-char UUID, >25 (D-7); appended to url
  username: mysite
  password_enc: "..."           # stored encrypted/obfuscated at rest (FR-35)

boot_anonymise:                 # BootNotification fields rewritten before forwarding upstream (D-6/FR-21a)
  charge_point_vendor: Wallbox  # replaces "Easee ASA" (configurable, e.g. "Wallbox")
  charge_point_model: Pulsar    # replaces "Easee One"
  # firmware_version: ""        # optional: omit/empty = pass through the real value
  # serial_number: ""           # optional: omit/empty = pass through the real value

admin:
  username: admin
  password_hash: "$2a$..."      # bcrypt (D-4)
```

- **Load** at startup; **validate**: `proxied_id` must be "" or a member of
  `chargepoints`; warn if any CP ID > 25 chars (FR-5a).
- **Atomic save** on any admin change (FR-34): write `config.yaml.tmp`, `fsync`, `rename`.
- A single `sync.RWMutex` in the manager guards the in-memory config snapshot.

---

## 5. OCPP-J Framing (`internal/ocpp`)

OCPP-J frames are JSON arrays:

| Type | Shape |
|------|-------|
| CALL (2) | `[2, "<uid>", "<Action>", {payload}]` |
| CALLRESULT (3) | `[3, "<uid>", {payload}]` |
| CALLERROR (4) | `[4, "<uid>", "<errCode>", "<errDesc>", {details}]` |

- `Parse([]byte) (Frame, error)` → discriminates on the first element; extracts `uid`,
  and for CALLs the `action` and raw payload (`json.RawMessage`, kept for transparent
  relay so the proxied path never re-serialises payloads — FR-19).
- Builders: `Result(uid, payload)`, `CallError(uid, code, desc)`, `Call(uid, action, payload)`.
- Typed payloads only for the messages the proxy actively answers or inspects:
  `BootNotification`, `Heartbeat`, `Authorize`, `StartTransaction`, `StopTransaction`,
  `StatusNotification`, `MeterValues`, `DataTransfer`. Everything else stays as raw JSON.

---

## 6. Server & Routing (`cmd/proxy`, `internal/admin`)

One `http.Server` on `listen_addr`. `http.ServeMux`:

- `/admin/` and `/admin` → admin handler (more specific pattern wins in Go 1.22 mux).
- `/` (catch-all) → OCPP WebSocket handler; CP ID = trimmed URL path
  (`/<CP-ID>` at root, confirmed D-8).

WebSocket upgrade handler steps:

1. Extract CP ID from path; reject `""`/`admin` collisions.
2. Verify CP ID ∈ allow-list → else `HTTP 404` (no upgrade, FR-3).
3. Require `Sec-WebSocket-Protocol: ocpp1.6`; echo it back (NFR-6). Reject otherwise.
4. Accept the WebSocket (coder/websocket `Accept` with subprotocol).
5. Handle duplicate CP ID: close any existing session for that ID first (Q6 default:
   replace stale).
6. Classify: `cpid == manager.ProxiedID()` → `ProxySession`, else `LocalSession`.
7. Run the session bound to a `context.Context`; register/unregister in the manager.

---

## 7. Sessions (`internal/session`)

Common: each session owns its downstream conn, a `context.Context` (cancel → tears down
everything), and a **liveness timer** reset on every inbound downstream frame; expiry at
`2 × heartbeat_interval` closes the session (FR-24).

### 7.1 LocalSession (auto-authorise — §5.3)

Single read loop; per CALL action:

| Action | Response | State effect |
|--------|----------|--------------|
| `BootNotification` | `Accepted`, `interval`, `currentTime` (FR-9) | mark online |
| `Heartbeat` | `currentTime` (FR-13) | bump lastHeartbeat |
| `Authorize` | `idTagInfo.status=Accepted` (FR-10) | — |
| `StartTransaction` | `Accepted` + new `transactionId` (FR-11) | open session, meterStart |
| `StopTransaction` | `Accepted` (FR-12) | close session, energy = meterStop−meterStart |
| `StatusNotification` | empty conf (FR-14); **auto-start** on Preparing (FR-16a) | update connector status |
| `MeterValues` | empty conf (FR-14) | update energy/power (FR-37) |
| `DataTransfer` | `status=Accepted` (FR-14; policy Q3a) | — |
| other | conf if benign else `CALLERROR NotSupported` (FR-15, Q7) | — |

- `transactionId` from a process-wide `atomic.Int64` counter.
- **Auto-start (D-11/FR-16a):** on `StatusNotification` `Preparing` (connectorId ≥ 1) with
  no active transaction and `local_auto_start.enabled`, the handler also emits a
  `RemoteStartTransaction` (idTag from config, default `PROXY`), once per Preparing episode
  (re-armed when the connector leaves Preparing). This is the sole command the proxy sends
  to a local CP; otherwise local handling is passive (FR-16). The handler returns a slice
  of frames so the confirmation and the auto-start CALL are both written.

### 7.2 ProxySession (relay — §5.4)

On start (downstream is the proxied CP):

1. Dial upstream (`internal/upstream`): compose `remote.url` + `/` + `upstream_id`
   (e.g. `wss://csms.example.com/a3f71c92-…-2c7a5e0b93d4`, D-8), header
   `Authorization: Basic base64(user:pass)`, subprotocol `ocpp1.6`, TLS verify on, dial
   deadline = `upstream_timeout` (FR-17, FR-18, D-7).
2. If dial fails → close downstream (Easee reconnects later, FR-27/Q9 default).
3. Spawn two relay goroutines under the shared context:
   - **downstream → upstream:** for each frame, **observe** for telemetry, then forward
     verbatim — **except** `BootNotification.req`: rewrite `chargePointVendor` /
     `chargePointModel` (and optionally `firmwareVersion` / `chargePointSerialNumber`) to
     the configured `boot_anonymise` values, then forward the rewritten frame upstream
     (FR-21a, D-6). Each Easee frame resets the downstream idle deadline.
     (Boot is the first frame after the WS handshake, so the read loop queues frames until
     the upstream dial completes, then forwards them in order.)
   - **upstream → downstream:** forward verbatim — including all CSMS-initiated commands
     (RemoteStart/Stop, Reset, ChangeConfiguration, TriggerMessage, …), which pass through
     untouched so the remote retains full control (D-10). On `BootNotification.conf`, read
     its `interval` and set this CP's liveness window to 2× it (FR-24, Appendix A showed
     `interval:10`). Each received frame resets the upstream idle deadline; observe `*.conf`
     for telemetry.
4. **Transparent IDs:** uids/payloads pass unchanged (FR-19/FR-20) — treat message IDs as
   **opaque strings** (Appendix A: Easee uses numeric IDs, CSMS uses UUIDs; both must relay
   untouched). Handle `CALLERROR` (type 4) frames too. The CP-ID difference lives only in
   the dial URL (D-7); the only payload the proxy rewrites is BootNotification (item 3).
5. **DataTransfer vendorId** (FR-21b): pluggable filter hook, default pass-through. The
   capture showed no DataTransfer and manufacturer only in boot (Q3a resolved), so this is
   a safety net, not an active transform.

Teardown wiring:

- Downstream close/err → cancel context → upstream closed (FR-22).
- Upstream close/err → cancel context → downstream closed (FR-23).
- Downstream liveness expiry (2× heartbeat) → cancel both (FR-24).
- Upstream idle deadline: no frame from the CSMS within **2× heartbeat interval** → cancel
  both (FR-25). This subsumes "CSMS stopped answering heartbeats"; no WS ping keepalive
  (Q4 resolved). `upstream_timeout` is used only for the initial dial.
- On cancel, both conns get a proper WebSocket close; all goroutines/timers drain (FR-26).

---

## 8. Manager & Role Switching (`internal/manager`)

Central object holding: config snapshot, `map[cpid]*Session` (active), the current proxied
session pointer, and `map[cpid]*CPState`. Guarded by `RWMutex`.

`SetProxiedID(newID)` (called from the dashboard, FR-8/FR-32b, D-9):

1. Persist config change (atomic save).
2. If an old proxied session exists → tear it down (cancel → closes **upstream + old
   downstream**).
3. If `newID` has a live session (a `LocalSession`) → **drop its downstream connection**
   too, so it reconnects and is re-classified as a `ProxySession`.
   → Net effect: all three affected connections drop (upstream, old Easee downstream, new
   Easee downstream) — D-9.
4. New classification takes effect on each unit's next connect; no restart (FR-30). Both
   Easees auto-reconnect (old → auto-authorised, new → proxied).

Because Easee units auto-reconnect within seconds, a role switch converges quickly without
operator action beyond the click.

---

## 9. Live State & Telemetry (`internal/state`)

```go
type CPState struct {
    ID              string
    Role            string     // "proxied" | "local"
    DownstreamUp    bool
    UpstreamUp      bool       // proxied only
    LastMessage     time.Time
    LastHeartbeat   time.Time
    ConnectorStatus string     // from StatusNotification
    TxnActive       bool
    TxnID           int
    IdTag           string
    TxnStart        time.Time
    MeterStartWh    int
    EnergyWh        int        // running: latest register − start
    PowerW          float64    // latest sampled, if present
    LastStopReason  string
}
```

- Updated by **observer functions** invoked from both session types on relevant messages
  (FR-36/FR-37). For the proxied CP, observation is read-only alongside the relay (FR-38).
- Energy from `MeterValues` `Energy.Active.Import.Register`, **honouring each sampled
  value's explicit `unit`** (Appendix A: Easee sends this register in **kWh**, power in
  **W** — do not assume Wh). Normalise to a common internal unit (Wh) on ingest. The
  register is cumulative/lifetime, so session energy = current − value-at-start (or
  `meterStop − meterStart`). Power/current/voltage from sampled values if present.
- In-memory only; reset on restart (FR-39). Rendered kWh/kW/local-time in the UI.

---

## 10. Admin UI (`internal/admin`, `web/templates`)

Auth (FR-31, D-4): login form → bcrypt-verify against `admin.password_hash` → signed,
HTTP-only session cookie (in-memory session store). All `/admin` routes require a valid
session; unauth redirects to `/admin/login`.

Pages (dashboard-first IA, FR-32b…FR-32e):

| Route | Purpose | Frequency |
|-------|---------|-----------|
| `GET /admin` | **Dashboard** — table of every CP with live state (§9) and a one-click "manage remotely" selector (radio incl. *None*). Meta-refresh ~5 s (FR-40). | daily |
| `POST /admin/proxied` | Set proxied CP → `manager.SetProxiedID` (with confirm, FR-32e) | daily |
| `GET/POST /admin/chargepoints` | Add / list / remove CP IDs; warn if >25 chars (FR-5a) | rare |
| `GET/POST /admin/remote` | Remote URL / upstream ID / username / password (write-only field, FR-32a) | rare |
| `GET/POST /admin/account` | Change admin password (re-hash) | rare |
| `GET/POST /admin/login`, `POST /admin/logout` | Session auth | — |

Dashboard row shows: CP ID, role badge (proxied/auto), downstream ●, upstream ● (proxied),
connector status, active-txn + kWh this session, last-heartbeat age. The proxied CP is
visually highlighted (FR-32c).

---

## 11. Concurrency, Shutdown, Safety

- One read goroutine per WS; child goroutines under a per-session `context.Context`.
- All timers via `context`-aware `time.Timer`; every path cancels on session end (FR-26).
- `sync.RWMutex` for manager maps/config; state updates are short critical sections.
- Graceful shutdown: SIGTERM → stop accepting, cancel all session contexts, close server.
- No panics escape a session goroutine (recover → log → close that session only, NFR-3).

---

## 12. Testing Strategy

- **ocpp:** table-tests for Parse/Build round-trips incl. malformed frames.
- **LocalSession:** drive CALLs, assert exact CALLRESULTs and state transitions.
- **Liveness:** inject a `Clock` interface (fake clock) to test 2×-heartbeat and
  upstream-timeout cutoffs deterministically (no real sleeps).
- **ProxySession:** spin an in-process mock CSMS (`httptest` + coder/websocket) and a mock
  CP client; assert:
  - frames relay verbatim both ways (FR-19/20),
  - **BootNotification reaches the mock CSMS with anonymised vendor/model** (never the real
    "Easee ASA"/"Easee One"), and the CSMS's conf (interval/status) relays back to the CP
    (D-6/FR-21a),
  - downstream close ⇒ upstream closes and vice-versa (FR-22/23),
  - upstream idle > 2× heartbeat ⇒ both close (FR-25).
- **Role switch:** assert all three connections drop — upstream, old-proxied downstream,
  and newly-selected downstream (FR-8/D-9).
- **Config:** load/validate/atomic-save round-trip; reject `proxied_id` not in list.
- **Admin:** auth required; password hashing; proxied-CP POST invokes manager.

---

## 13. Deployment (NFR-1)

- `go build ./cmd/proxy` → single static binary.
- `deploy/easee-proxy.service` (systemd): `-config /etc/easee-proxy/config.yaml`, run as a
  dedicated non-root user, `Restart=on-failure`.
- Bind `listen_addr` to the LAN interface; document that `/admin` is plain HTTP and MUST be
  reachable only from trusted LAN (NFR-5) — since the port is shared and downstream is
  plaintext by design (D-2), network-level isolation is the control.
- `config.example.yaml` + README with first-run steps (set admin password → add CP IDs →
  configure remote → pick proxied CP on the dashboard).

---

## 14. Milestones (suggested delivery order)

1. **Skeleton** — module, config load/save+validate, one-port HTTP+WS server, allow-list,
   subprotocol negotiation, `/admin` route reservation. *(FR-1–5a, FR-33/34)*
2. **Local auto-authorisation** — full CSMS responder, per-CP state, downstream liveness.
   *(§5.3, FR-9–16, FR-24, FR-36)*
3. **Admin dashboard + settings** — auth, dashboard with live state, role selector,
   settings pages. *(§5.6, §5.8 display, FR-28–32e, FR-40)*
4. **Upstream proxy relay** — dialer, bidirectional relay, BootNotification vendor
   anonymisation, ID mapping. *(§5.4, FR-17–21b, D-6/D-7)*
5. **Lifecycle cross-linking** — downstream/upstream teardown, upstream heartbeat timeout,
   role-switch teardown. *(FR-22, 23, 25, 27, FR-8)*
6. **Telemetry polish** — MeterValues energy/power parsing, dashboard formatting. *(FR-37)*
7. **Hardening** — full test suite, structured logging, systemd unit, README, example config.

Milestones 1–3 deliver a fully working *local-only* proxy (no remote) early; 4–6 add
proxying; 7 productionises.

---

## 15. Open Items Feeding the Build

All design questions are resolved (Q1/D-8, Q3, Q3a, Q4, Q5/D-9, Q6, Q7, Q8/D-10, Q9,
Q11–14). Boot handling was revised to **anonymise-and-forward** (D-6) after the packet
capture (Appendix A) showed the CSMS's provisioning depends on BootNotification. Nothing
outstanding blocks the build.
```
