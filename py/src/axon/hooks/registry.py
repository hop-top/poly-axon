"""Mirrors hop.top/axon/hooks's Register/For/Registered: a process-wide map
from canonical host name to its codec, populated by each host module's own
side-effecting registration (see hosts/all.py) rather than a hard-coded
table here.
"""

from __future__ import annotations

from axon.hooks.codec import Codec

_codecs: dict[str, Codec] = {}


def register(c: Codec) -> None:
    """Adds a codec to the registry. Raises on a duplicate host -- a
    programming error caught at load time, mirroring Go's Register panic.
    """
    host = c.host()
    if host in _codecs:
        raise RuntimeError(f"axon/hooks: codec for {host} registered twice")
    _codecs[host] = c


def codec_for(host: str) -> Codec | None:
    """Returns the codec for a canonical host name, or None if none is registered."""
    return _codecs.get(host)


def registered_hosts() -> list[str]:
    """The sorted list of hosts with a registered codec."""
    return sorted(_codecs.keys())
