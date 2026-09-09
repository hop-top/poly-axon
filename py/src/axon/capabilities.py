"""A host's capabilities.yaml: which canonical events it supports natively,
approximately, synthetically, or not at all.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Literal

import yaml

from axon._spec import read_spec_file
from axon.errors import SchemaError, UnknownHostError
from axon.events import Event
from axon.registry import get
from axon.schema import validate

# How a host supports a canonical event.
Level = Literal["native", "close", "synthesized", "unsupported"]


@dataclass(frozen=True)
class Pair:
    """A host-side event name mapped to a canonical event."""

    host_event: str
    event: Event
    note: str | None = None


@dataclass(frozen=True)
class Recipe:
    """How a host synthesizes an event it lacks natively.

    Every field is optional (contract notes S2b): ``technique`` and
    ``cost`` absent means the spec does not classify that row -- never
    default them to a sentinel that participates in matching logic.
    """

    technique: str | None = None
    source: str | None = None
    via: str | None = None
    pattern: str | None = None
    cost: str | None = None
    notes: str | None = None


@dataclass(frozen=True)
class Capabilities:
    """A host's parsed capabilities.yaml."""

    host: str
    native: list[Pair] = field(default_factory=list)
    host_version_range: str | None = None
    close: list[Pair] | None = None
    synthesized: dict[Event, Recipe] | None = None
    unsupported: list[Event] | None = None


def _to_pair(raw: dict) -> Pair:
    return Pair(host_event=raw["host_event"], event=raw["event"], note=raw.get("note"))


def _to_recipe(raw: dict) -> Recipe:
    return Recipe(
        technique=raw.get("technique"),
        source=raw.get("source"),
        via=raw.get("via"),
        pattern=raw.get("pattern"),
        cost=raw.get("cost"),
        notes=raw.get("notes"),
    )


def load_capabilities(host: str) -> Capabilities:
    """Loads and validates the bundled capabilities.yaml for a hooked host.

    Raises UnknownHostError ONLY when ``host`` is not a name axon
    recognizes at all (canonical names only, matching hooks.LoadCapabilities's
    ErrUnknownHost branch in Go). A recognized host with no
    capabilities.yaml -- every identity-only host, e.g. amp -- raises
    SchemaError instead: the name is fine, but there is nothing to load.
    These are never conflated: a caller must be able to branch on "this
    name is garbage" (UnknownHostError) versus "this host has no hook
    contract" (SchemaError). Never returns an empty Capabilities value on
    failure.
    """
    if get(host) is None:
        raise UnknownHostError(host)

    rel_path = f"hosts/{host}/capabilities.yaml"
    try:
        raw = read_spec_file(rel_path)
    except OSError as err:
        # host is recognized (checked above); a missing/unreadable
        # capabilities.yaml is a load-shaped failure, not an unknown-host
        # one -- every identity-only host (e.g. amp) hits this path and is
        # NOT "unknown". Matches hooks.LoadCapabilities in Go: ErrUnknownHost
        # is returned only from axon.Get's not-found branch; a failure from
        # LoadSpecYAML (missing file, parse error, schema violation) always
        # propagates as its own load error, never re-labeled as
        # unknown-host.
        raise SchemaError(f"{host}: no capabilities.yaml at {rel_path} ({err})") from err

    try:
        doc = yaml.safe_load(raw)
    except yaml.YAMLError as err:
        raise SchemaError(f"parse {rel_path}: {err}") from err

    validate(rel_path, "capabilities.schema.json", doc)

    synthesized_doc = doc.get("synthesized")
    unsupported_doc = doc.get("unsupported")
    close_doc = doc.get("close")

    return Capabilities(
        host=doc["host"],
        native=[_to_pair(p) for p in doc["native"]],
        host_version_range=doc.get("host_version_range"),
        close=[_to_pair(p) for p in close_doc] if close_doc is not None else None,
        synthesized=(
            {event: _to_recipe(recipe) for event, recipe in synthesized_doc.items()}
            if synthesized_doc is not None
            else None
        ),
        unsupported=list(unsupported_doc) if unsupported_doc is not None else None,
    )


def level(c: Capabilities, e: Event) -> tuple[Level | None, bool]:
    """The support level for e in c, and whether e is classified at all."""
    if any(p.event == e for p in c.native):
        return "native", True
    if c.close and any(p.event == e for p in c.close):
        return "close", True
    if c.synthesized and e in c.synthesized:
        return "synthesized", True
    if c.unsupported and e in c.unsupported:
        return "unsupported", True
    return None, False


def recipe(c: Capabilities, e: Event) -> Recipe | None:
    """The synthesis recipe for e, when e is synthesized."""
    if c.synthesized is None:
        return None
    return c.synthesized.get(e)


def host_event(c: Capabilities, e: Event) -> str | None:
    """The host-side event name for a native or close mapping."""
    for p in c.native:
        if p.event == e:
            return p.host_event
    if c.close:
        for p in c.close:
            if p.event == e:
                return p.host_event
    return None
