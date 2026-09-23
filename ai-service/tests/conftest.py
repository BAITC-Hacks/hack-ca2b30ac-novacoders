import json
import os
from pathlib import Path
import socket

import httpx
import pytest

from app.settings import Settings

ROOT = Path(__file__).resolve().parents[1]


@pytest.fixture(autouse=True)
def offline_and_isolated(monkeypatch):
    # Remove environment settings without inspecting/printing their values.
    for name in list(os.environ):
        if name.startswith(("AI_", "OPENAI_", "NVIDIA_")) or name == "ALLOW_EXTERNAL_AI":
            monkeypatch.delenv(name)

    def no_network(*args, **kwargs):
        raise AssertionError("Network access forbidden in ordinary tests")

    monkeypatch.setattr(socket.socket, "connect", no_network)
    monkeypatch.setattr(socket.socket, "connect_ex", no_network)
    monkeypatch.setattr(socket, "getaddrinfo", no_network)
    monkeypatch.setattr(httpx.AsyncClient, "send", no_network)


@pytest.fixture
def payload():
    return json.loads((ROOT / "contracts/legacy.request.example.json").read_text())


@pytest.fixture
def local_settings():
    return Settings(ai_local_mode=True)


@pytest.fixture
def mvp_payload():
    return json.loads((ROOT / "contracts/request.example.json").read_text())
