"""Mirrors hop.top/axon/hooks's capabilities_test.go case for case, to the
extent this task's scope (identity + events, no codecs) covers it.
"""

from __future__ import annotations

import pytest

from axon.capabilities import load_capabilities
from axon.errors import SchemaError, UnknownHostError
from axon.events import native_events
from axon.registry import get, hooked_hosts, hosts


class TestCapabilities:
    def test_every_native_event_classified_exactly_once_per_hooked_host(self) -> None:
        """TestEveryNativeEventClassifiedOnce."""
        for h in hooked_hosts():
            caps = load_capabilities(h.name)
            for ev in native_events():
                n = 0
                if any(p.event == ev for p in caps.native):
                    n += 1
                if caps.close and any(p.event == ev for p in caps.close):
                    n += 1
                if caps.synthesized and ev in caps.synthesized:
                    n += 1
                if caps.unsupported and ev in caps.unsupported:
                    n += 1
                assert n == 1, f"{h.name}: event {ev} classified {n} times, want exactly 1"

    def test_claude_is_all_native(self) -> None:
        """TestClaudeIsAllNative."""
        caps = load_capabilities("claude")
        assert len(caps.native) == 26
        assert len(caps.synthesized or {}) == 0
        assert len(caps.unsupported or []) == 0

    def test_identity_only_hosts_have_no_capabilities_and_error_is_not_unknown_host(self) -> None:
        """TestIdentityOnlyHostsHaveNoCapabilities.

        Go's hooks.LoadCapabilities returns ErrUnknownHost ONLY from
        axon.Get's not-found branch (hooks/capabilities.go:95-97); a
        recognized host whose capabilities.yaml can't be loaded falls
        through to axon.LoadSpecYAML's own error (hooks/capabilities.go:99-101),
        which is never re-labeled as ErrUnknownHost. Verified directly
        against hooks/capabilities.go before pinning this split.
        """
        for h in hosts():
            if h.hooks:
                continue
            with pytest.raises(SchemaError):
                load_capabilities(h.name)
            try:
                load_capabilities(h.name)
            except UnknownHostError:
                pytest.fail(f"{h.name}: load_capabilities raised UnknownHostError")
            except SchemaError:
                pass

    def test_load_capabilities_raises_unknown_host_error_for_unrecognized_name(self) -> None:
        with pytest.raises(UnknownHostError):
            load_capabilities("not-a-real-host")

    def test_load_capabilities_distinguishes_unknown_host_from_recognized_but_no_capabilities(self) -> None:
        """amp is a real, recognized host (identity-only: hooks: false, no
        capabilities.yaml on disk) -- it must never be reported as unknown.
        """
        assert get("amp") is not None

        with pytest.raises(SchemaError) as exc_info:
            load_capabilities("amp")
        assert not isinstance(exc_info.value, UnknownHostError)
        assert "amp" in str(exc_info.value)
        assert "capabilities.yaml" in str(exc_info.value)

        with pytest.raises(UnknownHostError) as unknown_exc_info:
            load_capabilities("not-a-real-host")
        assert "not-a-real-host" in str(unknown_exc_info.value)
