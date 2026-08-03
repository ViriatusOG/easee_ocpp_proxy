"""Binary sensors for the Easee OCPP Proxy integration."""
from __future__ import annotations

from homeassistant.components.binary_sensor import (
    BinarySensorDeviceClass,
    BinarySensorEntity,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN
from .entity import ProxyEntity


async def async_setup_entry(
    hass: HomeAssistant, entry: ConfigEntry, async_add_entities: AddEntitiesCallback
) -> None:
    coordinator = hass.data[DOMAIN][entry.entry_id]
    entities: list[ProxyEntity] = []
    for cp_id in coordinator.data:
        entities.extend(
            [
                OnlineBinarySensor(coordinator, cp_id),
                ChargingBinarySensor(coordinator, cp_id),
            ]
        )
    async_add_entities(entities)


class OnlineBinarySensor(ProxyEntity, BinarySensorEntity):
    _attr_name = "Online"
    _attr_device_class = BinarySensorDeviceClass.CONNECTIVITY

    def __init__(self, coordinator, cp_id):
        super().__init__(coordinator, cp_id)
        self._attr_unique_id = f"{cp_id}_online"

    @property
    def is_on(self) -> bool:
        return bool(self._cp.get("online"))

    # Report online/offline regardless of downstream state (the CP row always exists).
    @property
    def available(self) -> bool:
        return self.coordinator.last_update_success and self._cp_id in self.coordinator.data


class ChargingBinarySensor(ProxyEntity, BinarySensorEntity):
    _attr_name = "Charging"
    _attr_device_class = BinarySensorDeviceClass.BATTERY_CHARGING

    def __init__(self, coordinator, cp_id):
        super().__init__(coordinator, cp_id)
        self._attr_unique_id = f"{cp_id}_charging"

    @property
    def is_on(self) -> bool:
        return self._cp.get("connector_status") == "Charging"
