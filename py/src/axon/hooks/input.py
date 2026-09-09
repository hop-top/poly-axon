"""The canonical, host-independent hook Input shape, mirroring
ts/src/hooks/input.ts in Python idiom.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Literal

# The canonical outcome a hook handler returns for an event.
Action = Literal["allow", "warn", "block", "rewrite"]


@dataclass
class Input:
    """The canonical, host-independent shape of a hook invocation's input.

    A Codec translates a host's native payload into an Input and back;
    fields the canonical set does not name land in ``extra``.
    """

    event: str
    session_id: str = ""
    cwd: str = ""
    tool_name: str | None = None
    tool_input: dict | None = None
    tool_response: dict | None = None
    prompt: str | None = None
    extra: dict | None = field(default=None)
