"""The canonical hook event catalog decoded from spec/events.yaml.

Event names are the wire vocabulary (contract notes S7) -- never
normalized, cased, or rewritten by this binding.
"""

from __future__ import annotations

from dataclasses import dataclass, field

import yaml

from axon._spec import read_spec_file
from axon.schema import validate

# Event is a plain string alias: a canonical hook event name from
# spec/events.yaml, carried through unchanged.
Event = str


@dataclass(frozen=True)
class Derivation:
    """How a derived event is synthesized from a native one."""

    from_: Event
    when: dict


@dataclass(frozen=True)
class PayloadField:
    """One payload field entry in an event's catalog description."""

    field: str
    type: str
    notes: str | None = None
    example: str | None = None
    required: bool | None = None
    format: str | None = None
    description: str | None = None
    enum: list[str] | None = None


@dataclass(frozen=True)
class EventInfo:
    """One events.yaml catalog entry."""

    name: Event
    category: str
    description: str
    direction: str
    blocking: bool
    payload: list[PayloadField] | None = None
    # None means native -- see effective_origin().
    origin: str | None = None
    extension_source: str | None = None
    derivation: Derivation | None = None


def effective_origin(info: EventInfo) -> str:
    """Returns info.origin, or "native" when absent."""
    return info.origin if info.origin is not None else "native"


def _to_payload_field(raw: dict) -> PayloadField:
    return PayloadField(
        field=raw["field"],
        type=raw["type"],
        notes=raw.get("notes"),
        example=raw.get("example"),
        required=raw.get("required"),
        format=raw.get("format"),
        description=raw.get("description"),
        enum=list(raw["enum"]) if raw.get("enum") is not None else None,
    )


def _load_events() -> list[EventInfo]:
    raw = read_spec_file("events.yaml")
    doc = yaml.safe_load(raw)
    validate("events.yaml", "events.schema.json", doc)

    out: list[EventInfo] = []
    for e in doc["events"]:
        derivation_doc = e.get("derivation")
        out.append(
            EventInfo(
                name=e["name"],
                category=e["category"],
                description=e["description"],
                direction=e["direction"],
                blocking=e["blocking"],
                payload=(
                    [_to_payload_field(p) for p in e["payload"]] if e.get("payload") is not None else None
                ),
                origin=e.get("origin"),
                extension_source=e.get("extension_source"),
                derivation=(
                    Derivation(from_=derivation_doc["from"], when=dict(derivation_doc["when"]))
                    if derivation_doc is not None
                    else None
                ),
            )
        )
    return out


_cached_events: list[EventInfo] | None = None


def events() -> list[EventInfo]:
    """The full catalog, in file order."""
    global _cached_events
    if _cached_events is None:
        _cached_events = _load_events()
    return _cached_events


def native_events() -> list[Event]:
    """The host-contract scope: events a host CLI can emit directly.

    Origin is the whole test, matching Go's NativeEvents() (contract notes
    S2c): an extension event is native to one CLI but not all, and a derived
    event is synthesized rather than emitted, so both are excluded. 26 of the
    32 catalog entries qualify.
    """
    return [e.name for e in events() if effective_origin(e) == "native"]
