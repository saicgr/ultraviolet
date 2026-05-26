"""Ultraviolet Python SDK — talk to the REST control plane + the PG-wire proxy.

Quick start:

    import uv
    client = uv.Client(api_key="uvk_...", base_url="https://api.ultraviolet.dev")
    customers = client.list_customers()
    rows = client.query("SELECT 1")  # uses psycopg under the hood
"""

from __future__ import annotations
import os
from typing import Any, Iterable
import urllib.request
import urllib.parse
import json


class Client:
    def __init__(self, api_key: str | None = None, base_url: str | None = None):
        self.api_key = api_key or os.getenv("UV_API_KEY")
        self.base_url = (base_url or os.getenv("UV_API_URL") or "http://localhost:8080").rstrip("/")
        self._token: str | None = None

    def _headers(self) -> dict[str, str]:
        h = {"Content-Type": "application/json"}
        if self._token:
            h["Authorization"] = f"Bearer {self._token}"
        return h

    def _request(self, method: str, path: str, body: Any = None) -> Any:
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(self.base_url + path, data=data, headers=self._headers(), method=method)
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read().decode())

    def login(self) -> None:
        if not self.api_key:
            raise RuntimeError("api_key required")
        out = self._request("POST", "/api/v1/auth/login", {"api_key": self.api_key})
        self._token = out["access_token"]

    def list_customers(self) -> list[dict]:
        if not self._token:
            self.login()
        return self._request("GET", "/api/v1/customers")

    def list_synced_tables(self, connection_id: str) -> list[dict]:
        if not self._token:
            self.login()
        return self._request("GET", f"/api/v1/connections/{connection_id}/synced-tables")

    def query(self, sql: str) -> Iterable[tuple]:
        """Run a SQL query through the PG-wire proxy. Requires `psycopg` installed."""
        try:
            import psycopg  # type: ignore
        except ImportError as e:
            raise RuntimeError("install psycopg to use Client.query") from e
        conn_str = os.getenv("UV_PROXY_DSN")
        if not conn_str:
            raise RuntimeError("set UV_PROXY_DSN, e.g. postgres://slug:apikey@localhost:5000/slug_bigquery")
        with psycopg.connect(conn_str) as conn, conn.cursor() as cur:
            cur.execute(sql)
            yield from cur.fetchall()


__all__ = ["Client"]
