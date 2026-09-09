"""The Codec protocol: translates between a host's native hook wire
format and the canonical Input/Decision types. Mirrors ts/src/hooks/codec.ts.
"""

from __future__ import annotations

from typing import Protocol

from axon.capabilities import Capabilities
from axon.hooks.decision import Decision
from axon.hooks.input import Input


class Codec(Protocol):
    def host(self) -> str: ...

    def encode_input(self, inp: Input) -> dict:
        """Builds the host's native hook stdin envelope, as a JSON value."""
        ...

    def decode_input(self, raw: dict) -> Input:
        """Parses a host's native hook stdin envelope (already-parsed JSON value)."""
        ...

    def encode_decision(self, d: Decision) -> tuple[object, int]:
        """Encodes a Decision into the host's native stdout/exit-code convention.

        Returns (stdout, exit).
        """
        ...

    def decode_decision(self, stdout: object, exit_code: int) -> Decision:
        """Decodes a host's stdout/exit-code convention back into a Decision."""
        ...

    def capabilities(self) -> Capabilities: ...
