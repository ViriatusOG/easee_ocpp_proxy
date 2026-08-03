"""Base entity for the Easee OCPP Proxy integration."""
from __future__ import annotations

from homeassistant.helpers.entity import DeviceInfo
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN
from .coordinator import ProxyCoordinator


class ProxyEntity(CoordinatorEntity[ProxyCoordinator]):
    """Common base: ties each entity to a chargepoint device."""

    _attr_has_entity_name = True

    def __init__(self, coordinator: ProxyCoordinator, cp_id: str) -> None:
        super().__init__(coordinator)
        self._cp_id = cp_id

    @property
    def _cp(self) -> dict:
        return self.coordinator.data.get(self._cp_id, {})

    @property
    def device_info(self) -> DeviceInfo:
        return DeviceInfo(
            identifiers={(DOMAIN, self._cp_id)},
            name=self._cp.get("name", self._cp_id),
            manufacturer="Easee OCPP Proxy",
            model="Chargepoint",
        )

    @property
    def available(self) -> bool:
        return super().available and self._cp_id in self.coordinator.data
