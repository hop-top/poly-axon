"""The canonical, host-independent hook Decision shape, mirroring
ts/src/hooks/decision.ts in Python idiom.

Decision.event's blank default (``""``) is deliberate, not merely
Python's dataclass convenience: a blank event means "the wire did not
reveal the event", produced deliberately by the claude and codex decoders
when PostToolUse and SessionStart block decisions are byte-identical on
the wire (they share the same top-level ``{"decision": "block", "reason":
...}`` shape with nothing left to disambiguate). An omitted event and an
explicit empty string behave identically on encode -- see the codecs'
own ``event or "PreToolUse"``-style fallback.
"""

from __future__ import annotations

from dataclasses import dataclass

from axon.hooks.input import Action


@dataclass
class Decision:
    """The canonical, host-independent shape of a hook handler's result.

    A Codec encodes it into a host's native stdout/exit-code convention
    and decodes that convention back into a Decision.
    """

    action: Action
    event: str = ""
    message: str = ""
    rewrite: object | None = None
    metadata: dict | None = None
