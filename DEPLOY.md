# Deploying easee-ocpp-proxy on ZimaOS (Docker)

Two ways to run it on a ZimaOS NAS:

1. **One-file install (recommended)** — paste a single YAML into ZimaOS'
   *Install Custom App* dialog; it pulls a pre-built multi-arch image
   (`viriatusog/easee-ocpp-proxy:latest`) from Docker Hub.
2. **Build on the NAS** — no registry involved; build the ~30 MB image
   locally from the `Dockerfile` in this folder.

---

## Option 1 — One-file install (Docker Hub image)

### 1. Install the app

In ZimaOS: **Apps → + → Install Custom App** → switch to the **YAML** tab →
paste the contents of [`zimaos-custom-app.yaml`](zimaos-custom-app.yaml) →
**Install**.

What the YAML does:

- runs `viriatusog/easee-ocpp-proxy:latest` (amd64 + arm64) on port `9000`
- mounts ZimaOS's app data folder as `/config` (the `appdata:` placeholder)
- restarts unless stopped
- the app card opens the admin UI directly (`/admin`)

### 2. First run

1. Open `http://<nas-ip>:9000/admin`. On first boot the container seeds
   `config.yaml` from the bundled example, so the UI is ready immediately.
2. Complete first-run setup: set the admin username/password (written to the
   persisted `config.yaml`).
3. On **Chargepoints**, confirm/add each charger's OCPP identity (shown in
   the Easee app).
4. Point each charger's OCPP endpoint at
   `ws://<nas-ip>:9000/<chargepoint-id>` (plain `ws://`, subprotocol `ocpp1.6`).
5. Optionally: remote CSMS + **proxied** role on the **Remote server** page,
   and an **API token** (Account → API token) for the Home Assistant
   integration.

Config edits from the admin UI persist in the app data folder — removing and
reinstalling the app does not lose them.

### Updating the app

Re-trigger the `publish` workflow in the GitHub fork (Actions → publish →
Run workflow); once the run is green, the app card can re-pull
`:latest` (or reinstall it from the same YAML). Your config is untouched.

---

## Option 2 — Build the image on the NAS

Use this if you'd rather not depend on the Docker Hub image.

### 1. Copy the repo to the NAS

```bash
scp -r repo/ admin@<nas-ip>:/home/admin/   # or upload a zip via ZimaOS Files
```

### 2. Build

In ZimaOS **Docker → Build** (create image from Dockerfile), paste
`repo/Dockerfile` and tag the result `easee-ocpp-proxy:local`. Terminal
equivalent:

```bash
cd ~/repo
docker build -t easee-ocpp-proxy:local .
```

### 3. Run via Compose

In ZimaOS **Docker → Compose**, upload `repo/docker-compose.yml` (service
`easee-ocpp-proxy`, port 9000, `/DATA/AppData/easee-ocpp-proxy/config`
mounted as `/config`), or:

```bash
cd ~/repo
docker compose up -d
docker logs -f easee-ocpp-proxy
```

> If your ZimaOS **Settings → Apps → App data location** is not `/DATA`, use
> that path in `docker-compose.yml` instead.

### 4. First run

Same as above: `http://<nas-ip>:9000/admin` → set the admin password (on this
route, copy `config.example.yaml` into the config folder first if the entry
point hasn't seeded it), add chargepoints, point the chargers at
`ws://<nas-ip>:9000/<id>`.

---

## Packaging files in this folder

- `Dockerfile` — multi-stage build (Go → Alpine, ~30 MB image, non-root user)
- `docker-entrypoint.sh` — seeds a first-run config, fixes `/config` ownership,
  drops to the `proxy` user
- `docker-compose.yml` — service definition for ZimaOS
- `zimaos-custom-app.yaml` — the one-file *Install Custom App* YAML
- `config.example.yaml` — documented config template
- `.github/workflows/publish.yml` — manual workflow that builds and pushes the
  multi-arch Docker Hub image

The Go build and full test suite were verified before packaging; the binary
serves `http://<host>:9000/admin` and the OCPP WebSocket port on one port.

## Notes

- **Port conflict:** change the published port in the YAML / compose file if
  something else on the NAS uses 9000 (the container side stays 9000).
- **Home Assistant:** copy `homeassistant/custom_components/easee_ocpp_proxy/`
  into your HA `config/custom_components/` and point it at
  `http://<nas-ip>:9000/api` with the API token (see `homeassistant/README.md`).
- **Security:** the admin UI and API are plain HTTP — keep port 9000 on your
  LAN only, never port-forward it.
- **Debugging:** set `DEBUG: "true"` in the service `environment` for verbose
  per-frame OCPP logging.
