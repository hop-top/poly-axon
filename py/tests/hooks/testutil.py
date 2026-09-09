"""Shared fixture-loading helper for the hooks test suite."""

from __future__ import annotations

import json
import os

from axon import spec_root


def fixture(host: str, name: str) -> object:
    """Reads and parses a golden fixture for `host`, by file name (e.g. "PreToolUse.block.json")."""
    path = os.path.join(spec_root(), "fixtures", "hosts", host, name)
    with open(path, encoding="utf-8") as f:
        return json.load(f)
