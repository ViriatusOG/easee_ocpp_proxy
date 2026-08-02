# Easee OCPP Proxy — Requirements Specification

**Status:** Draft v0.7 (added charging schedules FR-41 + device aliases FR-42 — D-12)
**Date:** 2026-08-02

### Confirmed design decisions (from review)

- **D-1** Downstream (Easee → proxy) is plaintext `ws://`. Upstream (proxy → CSMS) is
  `wss://`, and the proxy relays all requests/responses between them.
- **D-2** The `/admin` interface is served as **plain `http://`** (not a WebSocket) on the
  **same port** as the OCPP endpoint. No TLS on the listening port.
- **D-3** Upstream authentication is **HTTP Basic auth over TLS** (OCPP 1.6 Security
  Profile 2).
- **D-4** `/admin` authenticates with a **username + password**, password stored
  **hashed**.
- **D-5** The admin interface is an **HTML UI only** (form-based); no separate machine/JSON
  API. Configuration is persisted (YAML) and survives restart.
- **D-6** *(Revised after packet capture — see Appendix A.)* For the proxied CP,
  `BootNotification` SHALL be **forwarded upstream with its manufacturer-identifying fields
  anonymised**, rather than swallowed. `chargePointVendor` and `chargePointModel` (and,
  optionally, `firmwareVersion` and `chargePointSerialNumber`) are replaced with
  **configurable** values (e.g. vendor `"Wallbox"`) before the message is forwarded; the
  CSMS's boot response relays back to the Easee unchanged. Rationale: the capture proved
  the CSMS's provisioning burst and online-detection are triggered by BootNotification, so
  fully swallowing it would break remote management. Anonymising the vendor fields hides
  the manufacturer while preserving OCPP function. *(Supersedes the earlier
  full-suppression decision.)*
- **D-7** The proxy SHALL **map the downstream (Easee) CP ID to a distinct upstream (CSMS)
  CP ID**. This is a required feature, not a convenience: Easee restricts CP IDs to **≤25
  characters**, while many remote OCPP servers issue **longer** CP IDs. The proxy presents
  the short ID to the Easee (`ws://.../<easee-id>`) and connects upstream using the
  configured longer ID (`wss://.../<upstream-id>`). Because OCPP-J carries the identity
  only in the WebSocket URL path (never in message payloads), this mapping is handled
  entirely at the connection/URL layer and message payloads relay unchanged.
- **D-10** Message-handling policy, two paths:
  - **Proxied CP:** the proxy is a **transparent bidirectional relay** — every OCPP
    message in **both** directions passes unaffected **except** the anonymised vendor
    fields of `BootNotification` (D-6/FR-21a).
    This explicitly includes all CSMS-initiated commands (`RemoteStartTransaction`,
    `RemoteStopTransaction`, `Reset`, `ChangeConfiguration`, `UnlockConnector`,
    `TriggerMessage`, etc.); the remote server retains full control of the proxied Easee.
  - **Local (auto-authorised) CPs:** the proxy behaves **passively** — it responds to
    CP-initiated messages and does not originate command CALLs — **except** for the
    optional auto-start in D-11 (a `RemoteStartTransaction` to begin a charge on a CP that
    waits for backend authorisation). No other operator control of local CPs is provided.
- **D-12** *(Added after Wallbox testing — putting a charger on OCPP disables its own
  scheduler, so the proxy must own time-based charging.)* For local (auto-authorised) CPs,
  the proxy provides **schedule-gated charging** (FR-41): named windows defined centrally
  and assigned per device. Enforcement is **start/stop at window edges** — the proxy sends
  `RemoteStartTransaction` when the window opens with a car plugged in and waiting, and
  `RemoteStopTransaction` when the window closes during a charge (chosen over charging
  profiles for charger-agnostic simplicity). Windows are **local wall-clock time,
  DST-aware** (server local zone by default, optional IANA override), and **may cross
  midnight**. Chargepoints may also carry a friendly **alias** shown on the dashboard
  (FR-42). Scheduling and auto-start apply **only to locally-controlled CPs**; a proxied CP
  is governed entirely by the remote server, so neither the reconciler nor auto-start run
  for it.
- **D-11** *(Added after Wallbox hardware testing — see Appendix B.)* For local
  (auto-authorised) CPs, the proxy SHALL support an optional **auto-start**: when a
  connector reports `StatusNotification` status `Preparing` (connectorId ≥ 1) with no
  active transaction, and `local_auto_start.enabled` is true (default), the proxy sends a
  `RemoteStartTransaction` (with a configurable idTag, default `PROXY`) to begin the
  charge. It is issued **once per Preparing episode** (re-armed when the connector leaves
  Preparing). This is required because some chargers wait for backend authorisation and
  never self-start when online; it is the "active" resolution of Q8, and the sole command
  the proxy originates to a local CP (revising the earlier fully-passive stance in D-10).
- **D-9** When the proxied CP ID is changed (via the dashboard), the proxy SHALL drop all
  three affected connections: the upstream CSMS session, the previously-proxied Easee's
  downstream session, and the newly-selected Easee's downstream session. Both Easees then
  auto-reconnect into their new roles. This is the concrete realisation of FR-8.
- **D-8** URL scheme and roles, confirmed against a working Apache ws→wss reverse proxy
  the operator already runs (Q1 resolved):
  - **Downstream (Easee → proxy):** `ws://<proxy-host>/<easee-cp-id>` at the root path —
    e.g. `ws://proxy.example.lan/K9PZQ4RT`. No auth downstream; the allow-list is the
    only gate.
  - **Upstream (proxy → CSMS):** `wss://<csms-host>[/<base>]/<upstream-cp-id>` with
    `Authorization: Basic …` injected by the proxy — e.g.
    `wss://csms.example.com/a3f71c92-6b48-4e05-9d1f-2c7a5e0b93d4`. The upstream ID
    (a 36-char UUID here) far exceeds Easee's 25-char limit, which is exactly why the
    mapping in D-7 is required.
  - The upstream WebSocket URL is composed as `remote.url` (scheme + host + optional base
    path) with the configured `upstream_id` appended as the final path segment.

---

## 1. Purpose & Scope

The Easee OCPP Proxy is a service that runs on a local network and sits between one
or more Easee One chargepoints and a remote OCPP Central System. For each connecting
chargepoint it does one of two things:

1. **Local auto-authorisation** — acts as the Central System itself, automatically
   accepting and authorising all charge sessions without contacting any remote server.
2. **Transparent proxying** — for exactly one designated chargepoint, relays the OCPP
   session to a remote OCPP Central System over an authenticated, encrypted connection.

The proxy also exposes a management (`/admin`) interface, on the same listening port,
for runtime configuration.

**In scope:** OCPP 1.6J (JSON over WebSocket), connection lifecycle management,
local auto-authorisation, single-target proxying, admin API.

**Out of scope (unless raised in review):** OCPP 2.0.1, OCPP-J SOAP variant, load
balancing across multiple remote servers, smart charging / load management logic,
billing, persistent storage of transaction history.

### 1.1 Motivation (why this proxy exists)

Real-world constraints of the Easee One make direct connection to many remote OCPP
servers impractical; the proxy exists to bridge these gaps (all verified against real
hardware):

- **Certificate pain:** Easee's native secure OCPP (`wss://`) requires uploading a
  certificate into the unit, which is cumbersome and Easee's certificate validation
  rejects many otherwise-valid certificates. Terminating TLS in the proxy and letting the
  Easee speak plain `ws://` on the LAN removes this problem entirely. (A ws→wss bridge via
  Apache has already been proven to work in this arrangement.)
- **CP ID length limit:** Easee accepts CP IDs of **≤25 characters**, but many remote OCPP
  servers assign **longer** CP IDs. The proxy decouples the two by mapping a short Easee
  CP ID to the longer upstream CP ID (see D-7).
- **Manufacturer disclosure:** The proxy suppresses the only OCPP message that cites the
  manufacturer (`BootNotification`) from reaching the remote server (see D-6).

---

## 2. Terminology

| Term | Meaning |
|------|---------|
| **CP** | Chargepoint — an Easee One unit connecting to the proxy. |
| **CP ID** | Chargepoint identity string, presented by the CP in the WebSocket URL path. |
| **CSMS / Central System** | Remote OCPP server the proxy connects onward to. |
| **Downstream** | The connection between an Easee CP and the proxy (proxy acts as server). |
| **Upstream** | The connection between the proxy and the remote CSMS (proxy acts as client). |
| **Proxied CP** | The single CP whose session is relayed upstream. |
| **Local CP** | Any accepted CP that is auto-authorised locally (not proxied). |

---

## 3. Recommended Technology

**Recommendation: Go (Golang).**

Rationale:
- Excellent, mature WebSocket support (`nhooyr.io/websocket` / `gorilla/websocket`)
  and first-class concurrency (goroutines) for managing many simultaneous, long-lived
  connections and their liveness timers.
- Compiles to a single static binary with no runtime dependencies — trivial Linux
  deployment (copy binary + config, run under systemd), small footprint, suitable for
  a Raspberry Pi or small VM on the local network.
- Strong standard library for HTTP (serving `/admin` and the WebSocket endpoint on one
  port), TLS, and JSON.

Alternatives considered: Rust (more implementation effort), Python asyncio (heavier
runtime, GIL a non-issue at this scale but deployment is fiddlier), Node.js (viable but
weaker for long-running connection-lifecycle correctness). Go is the best fit for the
stated deployment and connection-management requirements.

---

## 4. System Context

```
   Easee One CP #1 ──ws (ocpp1.6)──┐
   Easee One CP #2 ──ws (ocpp1.6)──┤
   Easee One CP #N ──ws (ocpp1.6)──┤
                                    ▼
                          ┌───────────────────┐        wss (ocpp1.6 + auth)
                          │   OCPP Proxy       │ ───────────────────────────▶  Remote
                          │  (this service)    │        (proxied CP only)        CSMS
                          │  :PORT             │
                          │   /  = OCPP WS      │
                          │   /admin = mgmt UI │
                          └───────────────────┘
```

- Downstream links are **unencrypted** OCPP 1.6J (`ws://`).
- The upstream link is **encrypted and authenticated** (`wss://` + credentials).
- One listening port serves both the OCPP WebSocket endpoint and the `/admin` interface.

---

## 5. Functional Requirements

### 5.1 Downstream (Easee-facing) server

- **FR-1** The proxy SHALL listen on a single configurable TCP port and accept OCPP 1.6J
  WebSocket connections, performing the HTTP→WebSocket upgrade per RFC 6455 and
  negotiating the `ocpp1.6` WebSocket subprotocol.
- **FR-2** The CP ID SHALL be taken from the WebSocket request URL path (OCPP-J
  convention `ws://host:port/<CP-ID>` — see Open Question Q1 for exact path scheme).
- **FR-3** The proxy SHALL accept a downstream connection only if its CP ID appears in
  the configured list of acceptable CP IDs; otherwise it SHALL reject the upgrade
  (HTTP 401/404) and not open a WebSocket.
- **FR-4** Downstream connections SHALL be unencrypted (`ws://`). (TLS termination for
  downstream is out of scope unless raised.)
- **FR-5** The proxy SHALL support multiple simultaneous downstream connections from
  distinct CP IDs. Behaviour on a duplicate CP ID (same ID connecting twice) is defined
  in Q6.
- **FR-5a** Downstream (Easee) CP IDs SHALL be treated as **≤25 characters**, matching
  Easee's limit. The admin interface SHOULD warn if a configured downstream CP ID exceeds
  25 characters. The upstream CP ID has no such limit (see D-7).

### 5.2 Chargepoint classification

- **FR-6** Configuration SHALL designate **at most one** CP ID as the *proxied* CP. All
  other accepted CP IDs are *local* (auto-authorised).
- **FR-7** It SHALL be valid for **no** CP to be designated as proxied, in which case all
  accepted CPs are auto-authorised and no upstream connection is ever made.
- **FR-8** When the designated proxied CP ID is changed via the admin interface, the
  proxy SHALL immediately tear down the current upstream session (and the associated
  downstream session — see Q5) so that the change takes effect cleanly.

### 5.3 Local auto-authorisation (local CPs)

For CPs that are not proxied, the proxy itself acts as the Central System and responds to
OCPP 1.6 CALLs. At minimum:

- **FR-9** `BootNotification.req` → respond `Accepted` with a configurable heartbeat
  `interval` and the proxy's current time.
- **FR-10** `Authorize.req` → respond with `idTagInfo.status = Accepted`.
- **FR-11** `StartTransaction.req` → respond `Accepted`, allocating a unique
  `transactionId`.
- **FR-12** `StopTransaction.req` → respond `Accepted`.
- **FR-13** `Heartbeat.req` → respond with current time.
- **FR-14** `StatusNotification.req`, `MeterValues.req`, `DataTransfer.req` → respond
  with the appropriate empty/accepted confirmation so the CP considers the message
  handled.
- **FR-15** The proxy SHALL respond to any other/unknown CALL with a protocol-valid
  response (either a benign confirmation or a `CALLERROR`) so the CP is never left
  waiting. (Exact policy — Q7.)
- **FR-16** Behaviour toward local CPs is passive/responsive, with **one exception**: the
  optional auto-start `RemoteStartTransaction` (FR-16a). The proxy SHALL NOT issue any other
  Central-System-initiated commands (Reset, ChangeConfiguration, RemoteStop, etc.) to local
  CPs. *(D-10, as revised by D-11.)*
- **FR-16a** When `local_auto_start.enabled` is true (default), and a local CP reports a
  connector (connectorId ≥ 1) as `Preparing` with no active transaction, the proxy SHALL
  send one `RemoteStartTransaction` (configurable idTag, default `PROXY`) to start the
  charge, re-arming only after the connector leaves `Preparing`. *(D-11.)*

#### Scheduling & aliases (local CPs)

- **FR-41** The proxy SHALL support **charging schedules** for local (auto-authorised)
  CPs. A schedule is a named window with a **start** and **stop** time (HH:MM), defined
  centrally and **assigned per device**. Semantics (D-12):
  - (a) Times are **local wall-clock, DST-aware** — evaluated in the configured timezone
    (IANA name) or the server's local zone if unset.
  - (b) A window **may cross midnight** (start later than stop, e.g. 23:30 → 05:30).
  - (c) A CP with no assigned schedule may charge at any time.
  - (d) **Enforcement (start/stop at edges):** the proxy SHALL NOT auto-start a CP outside
    its window; when the window **opens** with a car plugged in and waiting the proxy SHALL
    send `RemoteStartTransaction`; when the window **closes** during an active transaction
    the proxy SHALL send `RemoteStopTransaction`.
  - (e) A configuration error (bad time/timezone/unknown schedule) SHALL **fail open**
    (charging allowed) rather than silently block charging.
- **FR-42** Each chargepoint MAY be given a friendly **alias** (e.g. "Garage Left") which
  the dashboard displays in place of the raw CP ID.

### 5.4 Upstream proxying (proxied CP)

- **FR-17** When the proxied CP connects downstream, the proxy SHALL establish an
  upstream WebSocket connection to the configured remote CSMS URL, using the configured
  **upstream CP ID** in the URL path, and the configured credentials. The upstream CP ID
  is independent of the Easee's downstream CP ID and MAY be longer than 25 characters
  (see D-7); the proxy maps between them at the connection/URL layer.
- **FR-18** Upstream authentication SHALL use HTTP Basic auth over TLS (`wss://`), i.e.
  OCPP 1.6 Security Profile 2, with the configured username and password. *(Confirmed —
  D-3.)*
- **FR-19** Once both links are up, the proxy SHALL relay OCPP messages **transparently
  in both directions** — CALL, CALLRESULT, and CALLERROR frames — without altering message
  IDs or payloads. This includes all CSMS-initiated command CALLs (RemoteStart/Stop, Reset,
  ChangeConfiguration, TriggerMessage, etc.); the only exception is `BootNotification`,
  whose manufacturer fields are anonymised before forwarding per FR-21a. CP ID lives only
  in the URL path, so payloads otherwise pass through unchanged (Q3 resolved).
  *(Policy — D-10.)*
- **FR-20** The proxy SHALL preserve OCPP message-ID correlation so that CALLRESULT/
  CALLERROR frames are matched to the correct originating CALL across the relay.
- **FR-21** While a CP is proxied, the proxy SHALL NOT auto-authorise it locally; the
  remote CSMS is the sole authority for that CP's sessions — **except** for the
  BootNotification handling in FR-21a.
- **FR-21a** For the proxied CP, the proxy SHALL rewrite the `BootNotification.req` payload
  before forwarding it upstream — replacing `chargePointVendor` and `chargePointModel`
  (and, if configured, `firmwareVersion` and `chargePointSerialNumber`) with the configured
  anonymisation values — then relay the CSMS's `BootNotification.conf` (status / interval /
  currentTime) back to the Easee unchanged. This is the **only** payload the proxy
  modifies; all other messages relay byte-for-byte. *(Confirmed — D-6, revised.)*
- **FR-21b** The capture (Appendix A) showed the manufacturer is cited **only** in
  `BootNotification`; no `DataTransfer` was emitted. The proxy SHALL nonetheless retain a
  pluggable filter for `DataTransfer.vendorId`, defaulting to **pass-through**, so a future
  leak can be handled without structural change (Q3a resolved).

### 5.5 Connection lifecycle & liveness

- **FR-22** If the proxied Easee CP disconnects (downstream close/error), the proxy SHALL
  close the corresponding upstream connection to the CSMS.
- **FR-23** If the remote CSMS disconnects (upstream close/error), the proxy SHALL close
  the corresponding downstream connection to the Easee CP.
- **FR-24** **Downstream liveness:** if no message (Heartbeat or any other) is received
  from a CP within **twice the applicable heartbeat interval**, the proxy SHALL close that
  downstream connection (and, if proxied, the upstream connection too). The applicable
  interval is: for **local** CPs, the proxy's configured value; for the **proxied** CP, the
  interval the CSMS returns in its `BootNotification.conf` (the capture showed `interval=10`
  — Appendix A), which the proxy observes and tracks per FR-21a.
- **FR-25** **Upstream liveness:** for the proxied CP, if no frame is received from the
  CSMS within **twice the heartbeat interval** — which, since CP heartbeats are relayed
  every interval and answered, is equivalent to "the CSMS stopped answering heartbeats" —
  the proxy SHALL close **both** the upstream and downstream connections. No separate
  WebSocket ping keepalive is used (Q4 resolved). The `upstream_timeout` value is used only
  for the initial upstream dial/connect.
- **FR-26** The proxy SHALL clean up all per-connection resources (goroutines, timers,
  buffers) on close, leaving no stale connections or leaked timers.
- **FR-27** The proxy SHOULD, on transient upstream failure, allow the Easee CP to
  reconnect and re-establish the upstream link on its next connection attempt (Easee CPs
  auto-reconnect). Automatic upstream reconnection with an existing live downstream link —
  Q9.

### 5.6 Admin interface (`/admin`)

- **FR-28** The proxy SHALL serve a management interface at path prefix `/admin` on the
  **same port** as the OCPP endpoint, as plain `http://` (a normal HTML web interface, not
  a WebSocket). *(Confirmed — D-2.)*
- **FR-29** The admin interface SHALL allow:
  - (a) Adding, listing, and removing acceptable CP IDs.
  - (b) Configuring the remote CSMS URL, username, password, and upstream CP ID.
  - (c) Selecting which single CP ID (or none) is proxied; changing it triggers FR-8.
- **FR-30** Admin actions that change configuration SHALL take effect at runtime without
  a restart.
- **FR-31** The admin interface SHALL require authentication with an administrator
  **username and password**; the password SHALL be stored **hashed** (e.g. bcrypt/argon2)
  and never displayed. It MUST NOT be usable anonymously given it exposes/edits remote
  credentials. *(Confirmed — D-4.)*
- **FR-32** The admin interface SHALL be an **HTML, form-based UI** rendered server-side.
  No separate JSON/REST machine API is required. *(Confirmed — D-5.)*
- **FR-32a** The remote CSMS password field SHALL be write-only in the UI (settable but
  never rendered back in plaintext); a masked/"set" indicator SHALL be shown instead.

#### Information architecture (frequency-of-use driven)

The admin tasks differ greatly in how often they are performed, and the UI SHALL reflect
this:

- Adding/removing local Easee CP IDs and configuring the remote service are **rare**
  (setup-time) tasks.
- Choosing **which chargepoint is remotely managed** vs auto-authorised is an **almost
  daily** task.

- **FR-32b** The admin **home screen SHALL be a dashboard** that is the default landing
  page, optimised for the daily task: it SHALL list all configured chargepoints and let
  the administrator **select, in one or two clicks, which single CP is remotely managed
  (proxied)** and thereby which are auto-authorised. Selecting a new proxied CP (or
  "none") from the dashboard SHALL trigger the change/tear-down behaviour of FR-8 (and
  FR-5/Q5 for the affected downstream link).
- **FR-32c** The dashboard SHALL make the currently-proxied CP visually unambiguous, and
  make switching to a different CP (or to none) a direct action — no navigation into
  settings pages required.
- **FR-32d** The rare configuration tasks (CP ID add/remove, remote server URL/username/
  password/upstream CP ID, admin credentials) SHALL live on **separate settings pages**,
  reachable from the dashboard but not cluttering it.
- **FR-32e** Switching the proxied CP from the dashboard SHALL require a lightweight
  confirmation (since it disconnects a live remote session) but SHALL NOT require
  re-entering server configuration.

### 5.7 Configuration & persistence

- **FR-33** On startup the proxy SHALL load configuration from a file (format — Q13:
  YAML/TOML/JSON) containing: listen port, heartbeat interval, upstream timeout, list of
  acceptable CP IDs, proxied CP ID (or none), remote CSMS URL/username/password/upstream
  CP ID, and admin credentials.
- **FR-34** Configuration changes made via `/admin` SHALL be **persisted** so they survive
  a restart (confirm — Q13), written back atomically to the config store.
- **FR-35** Secrets (remote password, admin password) SHALL be stored hashed or encrypted
  where feasible, and never returned in plaintext by the admin API (Q10/Q11).

### 5.8 Per-chargepoint state tracking & telemetry

The proxy sits in the OCPP message path for every CP (as Central System for local CPs, as
relay for the proxied CP), so it can observe live state and surface it on the dashboard.

- **FR-36** For every configured chargepoint the proxy SHALL maintain and display live
  status derived from OCPP data, including at minimum:
  - **Role:** remotely-managed (proxied) or auto-authorised.
  - **Connection state:** downstream link up/down; for the proxied CP, upstream link
    up/down too; time of last message / last Heartbeat.
  - **Connector/CP status:** the latest `StatusNotification` state (e.g. Available,
    Preparing, Charging, SuspendedEV, Finishing, Faulted).
  - **Session state:** whether a transaction is active, its `transactionId`, `idTag`, and
    start time (from `StartTransaction`), or the last completed session (from
    `StopTransaction`, with stop reason).
- **FR-37** The proxy SHOULD display **energy and power telemetry** derived from OCPP where
  available: energy delivered this session (from `MeterValues`
  `Energy.Active.Import.Register`, or `meterStop − meterStart`), and, if reported, current
  power/current/voltage from `MeterValues` sampled values. The proxy SHALL honour each
  sampled value's explicit **`unit`** field and MUST NOT assume Wh: the captured Easee
  reports `Energy.Active.Import.Register` in **kWh** and `Power.Active.Import` in **W**
  (Appendix A). The energy register is a **cumulative lifetime** counter, so per-session
  energy is the delta from the value at transaction start.
- **FR-38** Telemetry SHALL be gathered **passively by observing messages** as they pass
  through — for the proxied CP the proxy reads (but does not alter) the relayed frames; for
  local CPs it reads the frames it is already answering. The proxy SHALL NOT inject extra
  OCPP requests solely to gather telemetry (no polled MeterValues), to keep behaviour
  transparent.
- **FR-39** Per-CP state is **in-memory/live** and reset on restart; persistent historical
  transaction logging is out of scope (per §1) unless later requested. Units SHALL be shown
  human-readably (kWh, kW, timestamps in local time).
- **FR-40** The dashboard SHALL refresh CP state on a short interval (e.g. via periodic
  page refresh or a lightweight poll) so an operator sees near-current status. (Given the
  HTML-only decision D-5, a simple meta-refresh or small poll is acceptable — no WebSocket
  admin channel required.)

---

## 6. Non-Functional Requirements

- **NFR-1 Platform:** Runs on Linux (x86-64 and ARM). Deployable as a single binary +
  config, e.g. under systemd.
- **NFR-2 Concurrency:** Handle up to **32** simultaneous downstream CPs (Q14) with
  per-connection isolation. (Easee sites are small; 32 is a comfortable ceiling.)
- **NFR-3 Resilience:** A fault on one connection MUST NOT affect others. No connection
  leaks under repeated connect/disconnect cycling.
- **NFR-4 Observability:** Structured logging of connection events, classification
  decisions, upstream state, and liveness timeouts; optional health endpoint.
- **NFR-5 Security:** Upstream credentials and admin credentials protected at rest;
  admin authenticated; principle of least exposure for the plaintext downstream port.
- **NFR-6 Standards conformance:** WebSocket per RFC 6455; OCPP 1.6J framing (CALL=2,
  CALLRESULT=3, CALLERROR=4) and subprotocol negotiation per the OCPP-J spec.

---

## 7. Assumptions

- A-1 Easee One units speak OCPP 1.6J over plain `ws://` and present their CP ID in the
  URL path (standard OCPP-J behaviour).
- A-2 The remote CSMS accepts OCPP 1.6J over `wss://` with HTTP Basic auth.
- A-3 CP ID appears only in the WebSocket URL, not inside message payloads, so relaying
  needs no payload rewriting **except** the BootNotification vendor-field anonymisation
  (FR-21a) — confirmed by Appendix A.
- A-4 Only one CP is ever proxied at a time; all others are auto-authorised.
- A-5 The site is small-scale (a handful of chargepoints).
- A-6 Admin operations include list/remove as well as add for CP IDs (implied by "management").

---

## 8. Open Questions / Elaborations Requested

The following need your input to finalise the design. Sensible defaults are proposed.
Resolved items are marked **[RESOLVED]** and recorded as decisions D-1…D-5 above.

- **Q2 — [RESOLVED]** Upstream = Basic auth over TLS, Profile 2 (D-3).
- **Q10 — [RESOLVED]** Admin auth = username + password, hashed (D-4).
- **Q11 — [RESOLVED]** Listening port is plaintext; `ws://` downstream, plain `http://`
  admin; upstream is `wss://` (D-1, D-2).
- **Q12 — [RESOLVED]** Admin = HTML form UI only, no JSON API (D-5).

Still open:

- **Q1 — [RESOLVED]** Endpoint is `ws://host:port/<CP-ID>` at the **root path**, `/admin`
  reserved. Confirmed against the working Apache reverse-proxy config (see D-8): downstream
  path `/K9PZQ4RT`, upstream `https://csms.example.com/<36-char-UUID>`.
- **Q3 — [RESOLVED]** CP ID lives only in the WebSocket URL path in OCPP-J, so the
  short→long ID mapping (D-7) is handled at the connection layer and **payloads relay
  unchanged**. No payload translation.
- **Q3a — [RESOLVED via pcap]** The capture (Appendix A) contained **no `DataTransfer`**;
  the manufacturer is cited **only** in `BootNotification`, which is now anonymised (D-6).
  A pass-through `DataTransfer.vendorId` filter hook is retained for safety (FR-21b). No
  further leak observed.
- **Q4 — [RESOLVED]** No separate WS ping keepalive. Liveness relies on the OCPP Heartbeat:
  anything longer than **twice the heartbeat interval** without traffic indicates a stale
  connection and triggers teardown (FR-24/FR-25).
- **Q5 — [RESOLVED]** On changing the proxied CP ID, the proxy drops **all three** affected
  connections so both units re-establish in their new roles (D-9): (1) the upstream CSMS
  connection, (2) the **previously** proxied Easee's downstream connection, and (3) the
  **newly** selected Easee's downstream connection. Both Easees auto-reconnect — the old
  one as auto-authorised, the new one as proxied.
- **Q6 — Duplicate CP ID:** If a CP ID that is already connected connects again, do we
  reject the new one, or replace the old (assume the old is stale)? (Default: replace old
  with new.)
- **Q7 — Unknown message policy (local CPs):** For CALLs the proxy doesn't explicitly
  model, respond with a generic accepted confirmation, or a `NotImplemented` CALLERROR?
  (Default: accept where a confirmation is harmless; CALLERROR `NotSupported` otherwise.)
- **Q8 — [RESOLVED]** See D-10. Proxied CP = fully transparent relay in both directions
  except BootNotification (CSMS commands pass through). Local CPs = passive; the proxy
  never originates commands to them.
- **Q9 — Upstream reconnect with live downstream:** If the CSMS drops but the Easee stays
  connected, should the proxy attempt upstream reconnection (with backoff) while keeping
  the Easee link, or always drop the Easee link (per FR-23) and let it reconnect?
  (Default: drop downstream per FR-23 for simplicity and to match your stated rule.)
- **Q13 — Config format & persistence:** Preferred config file format (YAML / TOML /
  JSON), and confirm admin changes must persist to it across restarts. (Default: YAML,
  persisted atomically.)
- **Q14 — [RESOLVED]** Maximum simultaneous Easee CPs is **≤ 32** (see NFR-2).

---

## Appendix A — Observed OCPP behaviour (packet capture, 2026-08-02)

Ground truth decoded from a real Easee One ↔ remote CSMS session (`easeereboot.pcap`,
plaintext `ws://10.0.0.10:80`, CP `K9PZQ4RT`, via the operator's Apache ws→wss bridge).
Recorded here because the capture file is transient. These facts drive D-6/FR-21a,
FR-24/25, and FR-37.

**Connection & framing**
- Downstream handshake: `GET /K9PZQ4RT HTTP/1.1` → `HTTP/1.1 101 Switching Protocols`
  (subprotocol `ocpp1.6`), confirming root-path CP ID (D-8).
- Message IDs are **opaque strings**, and the two ends use different schemes: the **Easee**
  uses short **numeric sequential** IDs (`42`,`43`,…`59`); the **CSMS** uses **UUIDs**.
  They never collide, and results echo the request ID. → the proxy MUST treat message IDs
  as opaque and never assume format/uniqueness across directions (FR-20).
- `CALLERROR` (type 4) frames occur in normal operation (Easee replied `NotSupported` to
  `SendLocalList`) → the relay/parser must handle type 4.

**BootNotification (the manufacturer citation — D-6)**
```
Easee → CSMS:  [2,"42","BootNotification",
   {"chargePointModel":"Easee One","chargePointVendor":"Easee ASA",
    "firmwareVersion":"343","chargePointSerialNumber":"K9PZQ4RT"}]
CSMS → Easee:  [3,"42",{"currentTime":"2026-08-02T16:24:41Z","interval":10,"status":"Accepted"}]
```
- Manufacturer/model/serial appear **only** here — no `DataTransfer` anywhere in the
  capture (Q3a). These are the fields FR-21a anonymises.
- **The CSMS's provisioning burst is triggered by this boot:** immediately after, the CSMS
  sent `GetConfiguration`, six × `ChangeConfiguration` (MeterValues config, sample
  intervals, min/maxSoC, current limit…), `SendLocalList`, and later a `TriggerMessage`
  (`StatusNotification`). This is *why* boot is forwarded (anonymised) rather than swallowed
  — swallowing it would suppress all of this and likely leave the charger unmanaged.
- The CSMS set **`interval:10`** (seconds) in the boot response → the proxied CP's liveness
  window is 2× the CSMS-supplied interval, not a proxy-configured one (FR-24).

**Telemetry (FR-37)**
```
[2,"45","MeterValues",{"connectorId":1,"meterValue":[{"timestamp":"…",
  "sampledValue":[
    {"value":"12272.427","measurand":"Energy.Active.Import.Register","unit":"kWh", …},
    {"value":"0.000","measurand":"Power.Active.Import","unit":"W", …}]}]}]
```
- Energy register is reported in **kWh** (cumulative lifetime ~12272 kWh), power in **W**.
  The proxy MUST read the explicit `unit` field and normalise — do **not** assume Wh.
- Connector statuses observed: `Available` → `Preparing` → `Available` (connectors 0 and
  1); no transaction (`StartTransaction`/`StopTransaction`) occurred in this capture.

---

## Appendix B — Observed behaviour on a Wallbox Pulsar (local auto-authorise test, 2026-08-02)

Live test of the local (auto-authorise) path against a Wallbox charger connected to the
proxy over plain `ws://`. Drives D-11/FR-16a.

- **The charger waits for backend authorisation.** When plugged in while online it reports
  connector `Preparing` and displays "Unlock your charger"; it does **not** self-start. A
  session begins only after the CSMS sends `RemoteStartTransaction`. → the local CSMS must
  auto-start (D-11).
- **Offline transactions are replayed on connect.** The first observed `StartTransaction`
  carried `idTag: "NoAuthorization"` and a `timestamp` *earlier than the OCPP connection* —
  i.e. a charge begun while offline, buffered, and flushed on reconnect. This is why an
  early test appeared to "self-start"; it was a replay, not online behaviour.
- **Transaction id correlation works:** the proxy assigned `transactionId` in
  `StartTransaction.conf` and the charger echoed it in subsequent `MeterValues`.
- **Telemetry:** periodic MeterValues (`context: Sample.Periodic`, ~30 s) include
  `Energy.Active.Import.Register` in **Wh** plus power/frequency/offered current; the
  once-a-minute `Sample.Clock` values carry only power. Session energy at stop =
  `meterStop − meterStart` (observed 35 Wh over a short session).
- **Status lifecycle:** `Available → SuspendedEV → Charging → SuspendedEV → Finishing →
  Available`, with a duplicate connectorId 0 / connectorId 1 pair per event.
- **No manufacturer leak beyond boot:** the `vendorId "com.wallbox"` appears only inside
  `StatusNotification`, which stays local for an auto-authorised CP.
