"""One axon-identified coding-assistant CLI, decoded from hosts/<name>/host.yaml."""

from __future__ import annotations

from dataclasses import dataclass, field

import yaml

from axon._spec import read_spec_file
from axon.schema import validate


@dataclass(frozen=True)
class StorePaths:
    """XDG-style root paths for a host's persistent stores."""

    config: str | None = None
    data: str | None = None
    cache: str | None = None
    state: str | None = None


@dataclass(frozen=True)
class ExitCodes:
    """A host's hook decision actions mapped to process exit codes.

    Absent entirely (the ``Host.exit_codes`` field is ``None``) on a host
    with no exit-code contract (contract notes S2a, e.g. OpenCode: hooks
    run in-process and signal block by throwing, not by process exit
    code). Never defaulted to zeros -- a binding that reads zeros for an
    absent contract would silently treat "no contract" as "exit 0 means
    allow".
    """

    allow: int
    block: int
    warn: int | None = None
    rewrite: int | None = None
    error: int | None = None


@dataclass(frozen=True)
class Host:
    """One axon-identified coding-assistant CLI."""

    name: str
    status: str
    aliases: list[str] = field(default_factory=list)
    binaries: list[str] = field(default_factory=list)
    store_roots: StorePaths = field(default_factory=StorePaths)
    config_file_patterns: list[str] = field(default_factory=list)
    project_key_strategy: str = ""
    hook_config_paths: list[str] = field(default_factory=list)
    exit_codes: ExitCodes | None = None
    envelope_discriminator: str = ""
    hooks: bool = False


def load_host_file(rel_path: str) -> Host:
    """Reads and validates one host.yaml (relative to the spec root, e.g.
    "hosts/claude/host.yaml") and maps it onto the Host shape. ``exit_codes``
    is populated only when the source document has an ``exit_codes`` key
    present -- checked by presence in the parsed YAML value, never
    defaulted -- per contract notes S2a.
    """
    raw = read_spec_file(rel_path)
    doc = yaml.safe_load(raw)
    validate(rel_path, "host.schema.json", doc)

    store_roots_doc = doc.get("store_roots") or {}
    exit_codes_doc = doc.get("exit_codes")

    return Host(
        name=doc["name"],
        status=doc["status"],
        aliases=list(doc.get("aliases") or []),
        binaries=list(doc["binaries"]),
        store_roots=StorePaths(
            config=store_roots_doc.get("config"),
            data=store_roots_doc.get("data"),
            cache=store_roots_doc.get("cache"),
            state=store_roots_doc.get("state"),
        ),
        config_file_patterns=list(doc.get("config_file_patterns") or []),
        project_key_strategy=doc.get("project_key_strategy") or "",
        hook_config_paths=list(doc.get("hook_config_paths") or []),
        exit_codes=(
            ExitCodes(
                allow=exit_codes_doc["allow"],
                block=exit_codes_doc["block"],
                warn=exit_codes_doc.get("warn"),
                rewrite=exit_codes_doc.get("rewrite"),
                error=exit_codes_doc.get("error"),
            )
            if exit_codes_doc is not None
            else None
        ),
        envelope_discriminator=doc.get("envelope_discriminator") or "",
        hooks=bool(doc.get("hooks") or False),
    )
