"""Mirrors hop.top/axon's registry_test.go case for case."""

from __future__ import annotations

from axon.registry import get, hooked_hosts, hosts, resolve


class TestRegistry:
    def test_hosts_count_is_17(self) -> None:
        """TestHostsCount."""
        assert len(hosts()) == 17

    def test_hosts_is_sorted_by_canonical_name(self) -> None:
        names = [h.name for h in hosts()]
        assert names == sorted(names)

    def test_get_rejects_aliases_only_resolve_is_alias_aware(self) -> None:
        """TestGetIsCanonicalOnly."""
        assert get("claude-code") is None
        assert get("gemini-cli") is None
        assert get("codex-cli") is None
        h = get("claude")
        assert h is not None
        assert h.name == "claude"

    def test_resolve_accepts_canonical_names_and_published_aliases(self) -> None:
        """TestResolveAliases."""
        cases = {
            "claude-code": "claude",
            "gemini-cli": "gemini",
            "codex-cli": "codex",
            "opencode": "opencode",
        }
        for alias, want in cases.items():
            h = resolve(alias)
            assert h is not None, f"resolve({alias})"
            assert h.name == want

    def test_resolve_of_an_unknown_name_returns_none(self) -> None:
        """TestResolveAliases."""
        assert resolve("nope") is None

    def test_aliases_are_unique_across_all_hosts_and_never_collide_with_canonical_name(self) -> None:
        """TestAliasUniqueness."""
        seen: dict[str, str] = {}
        for h in hosts():
            seen[h.name] = h.name
        for h in hosts():
            for a in h.aliases:
                owner = seen.get(a)
                assert owner is None, f"alias {a} of {h.name} collides with {owner}"
                seen[a] = h.name

    def test_hooked_hosts_count_is_8(self) -> None:
        """TestHookedHostsAreEight."""
        assert len(hooked_hosts()) == 8

    def test_hooked_hosts_all_carry_hooks_true(self) -> None:
        for h in hooked_hosts():
            assert h.hooks is True
