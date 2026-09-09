"""The host registry: every host.yaml under spec/hosts/, indexed by
canonical name and by published alias.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from axon._spec import list_spec_dir
from axon.host import Host, load_host_file


@dataclass
class _Registry:
    by_name: dict[str, Host] = field(default_factory=dict)
    alias_index: dict[str, str] = field(default_factory=dict)


def _load_registry() -> _Registry:
    reg = _Registry()
    for entry in list_spec_dir("hosts"):
        host = load_host_file(f"hosts/{entry}/host.yaml")
        reg.by_name[host.name] = host
        for alias in host.aliases:
            reg.alias_index[alias] = host.name
    return reg


_cached_registry: _Registry | None = None


def _registry() -> _Registry:
    global _cached_registry
    if _cached_registry is None:
        _cached_registry = _load_registry()
    return _cached_registry


def hosts() -> list[Host]:
    """Every host, sorted by canonical name."""
    return sorted(_registry().by_name.values(), key=lambda h: h.name)


def get(name: str) -> Host | None:
    """Looks up a host by canonical name only -- never an alias."""
    return _registry().by_name.get(name)


def resolve(name_or_alias: str) -> Host | None:
    """Accepts a canonical name or a published alias. The only alias-aware
    function in this module.
    """
    direct = _registry().by_name.get(name_or_alias)
    if direct is not None:
        return direct
    canonical = _registry().alias_index.get(name_or_alias)
    if canonical is None:
        return None
    return _registry().by_name.get(canonical)


def hooked_hosts() -> list[Host]:
    """Hosts with a hook surface (hooks: true)."""
    return [h for h in hosts() if h.hooks]
