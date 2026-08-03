# Home Assistant integration — Easee OCPP Proxy

A custom Home Assistant integration that exposes each chargepoint managed by the proxy
as an HA **device**, with view-only sensors and a **role selector**. It uses the proxy's
JSON API (local polling) and provides no admin/settings functions.

## Entities (per chargepoint device)

| Entity | Type | Notes |
|--------|------|-------|
| Role | `select` | proxied / always on / scheduled — *scheduled* only offered when a schedule is assigned; only one chargepoint can be proxied (enforced by the proxy) |
| Connector status | sensor | Available / Preparing / Charging / … |
| Power | sensor | kW |
| Session energy | sensor | kWh (resets each session) |
| Session | sensor | `active #<txn>` or `idle` |
| Schedule | sensor | e.g. `Offpeak (open)` / `(closed)` / `(paused)`, or `none` |
| Online | binary_sensor | downstream connection up |
| Charging | binary_sensor | connector is charging |

## Setup

1. In the proxy's admin UI, go to **Account → API token** and **Generate API token**. Copy it.
2. Copy `custom_components/easee_ocpp_proxy/` into your Home Assistant `config/custom_components/`
   directory (final path: `config/custom_components/easee_ocpp_proxy/`).
3. Restart Home Assistant.
4. **Settings → Devices & Services → Add Integration → “Easee OCPP Proxy”.**
5. Enter the proxy **Base URL** (e.g. `http://192.168.1.10:9000`) and the **API token**.

Each configured chargepoint appears as a device. Changing the **Role** select performs the
same switch as the web dashboard (and reconnects the chargepoint when switching to/from
proxied).

## Notes

- Polls every 15 seconds (local network).
- Chargepoints are enumerated when the integration loads; if you add a new chargepoint in
  the proxy, reload the integration to pick it up.
- The API is disabled unless a token is set, and every request must present it as
  `Authorization: Bearer <token>`. Keep the proxy on a trusted LAN.
