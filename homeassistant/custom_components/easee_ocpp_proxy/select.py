"""Role selector for the Easee OCPP Proxy integration.

Mirrors the web dashboard's role control: proxied / always on / scheduled, with
"scheduled" offered only when the chargepoint has a schedule assigned. The
"only one proxied" rule is enforced by the proxy itself.
"""
from __future__ import annotations

from homeassistant.components.select import SelectEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN, LABEL_MODES, MODE_LABELS
from .entity import ProxyEntity


async def async_setup_entry(
    hass: HomeAssistant, entry: ConfigEntry, async_add_entities: AddEntitiesCallback
) -> None:
    coordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities(RoleSelect(coordinator, cp_id) for cp_id in coordinator.data)


class RoleSelect(ProxyEntity, SelectEntity):
    _attr_icon = "mdi:tune-variant"
    _attr_name = "Role"

    def __init__(self, coordinator, cp_id):
        super().__init__(coordinator, cp_id)
        self._attr_unique_id = f"{cp_id}_role"

    @property
    def options(self) -> list[str]:
        modes = self._cp.get("modes") or ["proxied", "always_on"]
        return [MODE_LABELS[m] for m in modes if m in MODE_LABELS]

    @property
    def current_option(self) -> str | None:
        return MODE_LABELS.get(self._cp.get("role"))

    async def async_select_option(self, option: str) -> None:
        mode = LABEL_MODES.get(option)
        if mode:
            await self.coordinator.async_set_mode(self._cp_id, mode)
