"""Config flow for the Easee OCPP Proxy integration."""
from __future__ import annotations

import asyncio
from typing import Any

import voluptuous as vol

from homeassistant import config_entries
from homeassistant.data_entry_flow import FlowResult
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .const import CONF_HOST, CONF_TOKEN, DOMAIN


class ProxyConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Handle a config flow for Easee OCPP Proxy."""

    VERSION = 1

    async def async_step_user(self, user_input: dict[str, Any] | None = None) -> FlowResult:
        errors: dict[str, str] = {}
        if user_input is not None:
            host = user_input[CONF_HOST].rstrip("/")
            token = user_input[CONF_TOKEN]
            errors = await self._validate(host, token)
            if not errors:
                await self.async_set_unique_id(host)
                self._abort_if_unique_id_configured()
                return self.async_create_entry(
                    title=f"Easee OCPP Proxy ({host})",
                    data={CONF_HOST: host, CONF_TOKEN: token},
                )

        schema = vol.Schema(
            {
                vol.Required(CONF_HOST, default="http://"): str,
                vol.Required(CONF_TOKEN): str,
            }
        )
        return self.async_show_form(step_id="user", data_schema=schema, errors=errors)

    async def _validate(self, host: str, token: str) -> dict[str, str]:
        session = async_get_clientsession(self.hass)
        try:
            async with asyncio.timeout(10):
                resp = await session.get(
                    f"{host}/api/chargepoints",
                    headers={"Authorization": f"Bearer {token}"},
                )
        except Exception:  # noqa: BLE001
            return {"base": "cannot_connect"}
        if resp.status in (401, 403):
            return {"base": "invalid_auth"}
        if resp.status != 200:
            return {"base": "cannot_connect"}
        return {}
