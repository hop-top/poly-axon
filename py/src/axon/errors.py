"""The four contract sentinels, mirroring hop.top/axon's ErrUnknownHost
(axon package) and ErrUnknownHost / ErrUnsupportedEvent /
ErrUnsupportedAction / ErrSchema (axon/hooks package). This binding has one
axon package, so all four live together here.
"""

from __future__ import annotations


class UnknownHostError(Exception):
    """The host name is not one axon knows (canonical names only)."""

    def __init__(self, host: str) -> None:
        super().__init__(f"axon: unknown host: {host}")


class UnsupportedEventError(Exception):
    """The host has no wire shape for a canonical event -- its capability
    file marks it unsupported or synthesized, or does not classify it at
    all. Reserved for the hook-codec layer; not raised by this task.
    """

    def __init__(self, host: str, event: str) -> None:
        super().__init__(f"axon: event not supported by host: {host}: {event}")


class UnsupportedActionError(Exception):
    """A decision carries an action the host cannot express, or one outside
    the four canonical action constants. Reserved for the hook-codec layer;
    not raised by this task.
    """

    def __init__(self, message: str) -> None:
        super().__init__(f"axon: action not supported by host: {message}")


class SchemaError(Exception):
    """A payload does not match its schema, a spec file is malformed, or a
    spec file that must exist for a recognized host (e.g. a hooked host's
    capabilities.yaml) is missing or unreadable. Distinct from
    UnknownHostError: SchemaError always implies the host name itself
    resolved fine -- the failure is about a document, not an identity.
    """

    def __init__(self, message: str) -> None:
        super().__init__(f"axon: schema: {message}")
