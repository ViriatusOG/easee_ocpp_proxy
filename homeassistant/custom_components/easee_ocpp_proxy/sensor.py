"""Sensors for the Easee OCPP Proxy integration."""
from __future__ import annotations

from homeassistant.components.sensor import (
    SensorDeviceClass,
    SensorEntity,
    SensorStateClass,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import UnitOfEnergy, UnitOfPower
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
                ConnectorSensor(coordinator, cp_id),
                PowerSensor(coordinator, cp_id),
                EnergySensor(coordinator, cp_id),
                SessionSensor(coordinator, cp_id),
                ScheduleSensor(coordinator, cp_id),
            ]
        )
    async_add_entities(entities)


class ConnectorSensor(ProxyEntity, SensorEntity):
    _attr_icon = "mdi:ev-station"
    _attr_name = "Connector status"

    def __init__(self, coordinator, cp_id):
        super().__init__(coordinator, cp_id)
        self._attr_unique_id = f"{cp_id}_connector"

    @property
    def native_value(self):
        return self._cp.get("connector_status") or None


class PowerSensor(ProxyEntity, SensorEntity):
    _attr_name = "Power"
    _attr_device_class = SensorDeviceClass.POWER
    _attr_native_unit_of_measurement = UnitOfPower.KILO_WATT
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 2

    def __init__(self, coordinator, cp_id):
        super().__init__(coordinator, cp_id)
        self._attr_unique_id = f"{cp_id}_power"

    @property
    def native_value(self):
        return self._cp.get("power_kw")


class EnergySensor(ProxyEntity, SensorEntity):
    _attr_name = "Session energy"
    _attr_device_class = SensorDeviceClass.ENERGY
    _attr_native_unit_of_measurement = UnitOfEnergy.KILO_WATT_HOUR
    # Per-session energy resets to 0 at each new transaction start.
    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_suggested_display_precision = 3

    def __init__(self, coordinator, cp_id):
        super().__init__(coordinator, cp_id)
        self._attr_unique_id = f"{cp_id}_energy"

    @property
    def native_value(self):
        return self._cp.get("energy_kwh")


class SessionSensor(ProxyEntity, SensorEntity):
    _attr_icon = "mdi:counter"
    _attr_name = "Session"

    def __init__(self, coordinator, cp_id):
        super().__init__(coordinator, cp_id)
        self._attr_unique_id = f"{cp_id}_session"

    @property
    def native_value(self):
        cp = self._cp
        if cp.get("session_active"):
            return f"active #{cp.get('transaction_id', 0)}"
        return "idle"


class ScheduleSensor(ProxyEntity, SensorEntity):
    _attr_icon = "mdi:calendar-clock"
    _attr_name = "Schedule"

    def __init__(self, coordinator, cp_id):
        super().__init__(coordinator, cp_id)
        self._attr_unique_id = f"{cp_id}_schedule"

    @property
    def native_value(self):
        cp = self._cp
        sched = cp.get("schedule")
        if not sched:
            return "none"
        state = cp.get("schedule_state")
        return f"{sched} ({state})" if state else sched
