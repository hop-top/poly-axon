from __future__ import annotations

from axon.hooks import codec_for, registered_hosts
from axon.hooks.hosts import all as _all  # noqa: F401  (registers the four codecs)


class TestCodecRegistry:
    def test_codec_for_resolves_canonical_names_only_never_an_alias(self) -> None:
        assert codec_for("claude") is not None
        # "claude-code" is a published alias of claude, not a canonical
        # name -- codec_for must not resolve it (mirrors hooks.For in Go,
        # which keys its map by canonical host name only).
        assert codec_for("claude-code") is None

    def test_codec_for_returns_none_for_an_unknown_host(self) -> None:
        assert codec_for("not-a-real-host") is None

    def test_registered_hosts_is_exactly_the_four_hooked_hosts_with_a_codec_today_sorted(self) -> None:
        assert registered_hosts() == ["claude", "codex", "gemini", "opencode"]

    def test_every_registered_codec_host_matches_its_registry_key(self) -> None:
        for name in registered_hosts():
            codec = codec_for(name)
            assert codec is not None
            assert codec.host() == name
