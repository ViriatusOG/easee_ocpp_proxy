"""Data update coordinator for the Easee OCPP Proxy integration."""
from __future__ import annotations

import asyncio
import logging
from datetime import timedelta

from homeassistant.core import HomeAssistant
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from .const import DEFAULT_SCAN_INTERVAL, DOMAIN

_LOGGER = logging.getLogger(__name__)


class ProxyCoordinator(DataUpdateCoordinator):
    """Polls the proxy's JSON API and caches chargepoint state keyed by id."""

    def __init__(self, hass: HomeAssistant, host: str, token: str) -> None:
        super().__init__(
            hass,
            _LOGGER,
            name=DOMAIN,
            update_interval=timedelta(seconds=DEFAULT_SCAN_INTERVAL),
        )
        self._host = host.rstrip("/")
        self._token = token
        self._session = async_get_clientsession(hass)

    @property
    def host(self) -> str:
        return self._host

    def _headers(self) -> dict[str, str]:
        return {"Authorization": f"Bearer {self._token}"}

    async def _async_update_data(self) -> dict[str, dict]:
        try:
            async with asyncio.timeout(10):
                resp = await self._session.get(
                    f"{self._host}/api/chargepoints", headers=self._headers()
                )
                if resp.status in (401, 403):
                    raise UpdateFailed("authentication failed (check API token)")
                resp.raise_for_status()
                data = await resp.json()
        except UpdateFailed:
            raise
        except Exception as err:  # noqa: BLE001
            raise UpdateFailed(f"error talking to proxy: {err}") from err
        return {cp["id"]: cp for cp in data}

    async def async_set_mode(self, cp_id: str, mode: str) -> None:
        """Send a role/mode change, then refresh."""
        async with asyncio.timeout(10):
            resp = await self._session.post(
                f"{self._host}/api/chargepoints/{cp_id}/mode",
                headers=self._headers(),
                json={"mode": mode},
            )
            resp.raise_for_status()
        await self.async_request_refresh()
