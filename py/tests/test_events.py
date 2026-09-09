"""Mirrors hop.top/axon's events_test.go case for case."""

from __future__ import annotations

from axon.events import effective_origin, events, native_events


class TestEvents:
    def test_catalog_totals_32_events_native_events_is_26(self) -> None:
        """TestEventsCatalogLoads."""
        assert len(events()) == 32
        assert len(native_events()) == 26

    def test_non_native_events_are_excluded_from_native_events(self) -> None:
        """TestNonNativeEventsExcludedFromNativeScope.

        Origin is the whole native test, so every non-native event must be
        reachable through origin alone -- no category carve-out backs it up.
        """
        native = set(native_events())
        non_native = [e for e in events() if effective_origin(e) != "native"]
        assert len(non_native) == 6  # 4 extension + 2 derived
        for e in non_native:
            assert e.name not in native, f"{e.origin} event {e.name} must not appear in native_events"

    def test_every_event_declares_origin_explicitly(self) -> None:
        """TestEveryEventDeclaresOriginExplicitly.

        effective_origin() still defaults a missing origin to native, but
        nothing in the catalog relies on that fallback.
        """
        for e in events():
            assert e.origin is not None, f"event {e.name} omits origin"

    def test_native_events_is_exactly_the_origin_native_subset(self) -> None:
        """Contract notes 2c."""
        by_origin_alone = [e.name for e in events() if effective_origin(e) == "native"]
        assert by_origin_alone == native_events()
