"""Constants for the Easee OCPP Proxy integration."""

DOMAIN = "easee_ocpp_proxy"

CONF_HOST = "host"  # base URL, e.g. http://192.168.1.10:9000
CONF_TOKEN = "token"

DEFAULT_SCAN_INTERVAL = 15  # seconds

# Role modes exposed by the proxy API and their human-readable labels.
MODE_LABELS = {
    "proxied": "Proxied",
    "always_on": "Always on",
    "always_off": "Always off",
    "scheduled": "Scheduled",
    "synchronised": "Synchronised",
}
LABEL_MODES = {label: mode for mode, label in MODE_LABELS.items()}
