"""The hooks surface: canonical Input/Decision types, the Codec protocol,
and the registry lookup. Importing axon.hooks.hosts.all registers the four
codecs this task implements as a side effect (see that module).
"""

from __future__ import annotations

from axon.hooks.codec import Codec
from axon.hooks.decision import Decision
from axon.hooks.input import Action, Input
from axon.hooks.registry import codec_for, register, registered_hosts

__all__ = [
    "Action",
    "Input",
    "Decision",
    "Codec",
    "register",
    "codec_for",
    "registered_hosts",
]
