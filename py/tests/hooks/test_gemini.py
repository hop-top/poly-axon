"""Pins the eight task invariants against the gemini codec. Matched
against hooks/hosts/gemini/codec.go and codec_test.go.
"""

from __future__ import annotations

import pytest

from axon import UnsupportedActionError, UnsupportedEventError
from axon.hooks import Decision, Input
from axon.hooks import codec_for
from axon.hooks.hosts import all as _all  # noqa: F401

from .testutil import fixture


def _codec():
    c = codec_for("gemini")
    assert c is not None, "gemini codec not registered"
    return c


class TestGeminiCodec:
    def test_host_returns_the_canonical_name(self) -> None:
        assert _codec().host() == "gemini"

    def test_i2_encode_decision_raises_for_an_unrecognized_action(self) -> None:
        with pytest.raises(UnsupportedActionError):
            _codec().encode_decision(Decision(action="bogus"))  # type: ignore[arg-type]

    # Gemini has no native warn; nerv folds warn into decision:"allow" with
    # reason/systemMessage duplicated -- it must never become "deny".
    def test_i2_encode_decision_never_maps_warn_to_block(self) -> None:
        stdout, _exit = _codec().encode_decision(Decision(action="warn", message="careful"))
        assert stdout["decision"] == "allow"

    def test_i3_encode_decision_raises_for_an_event_gemini_does_not_classify(self) -> None:
        with pytest.raises(UnsupportedEventError):
            _codec().encode_decision(Decision(event="TeammateIdle", action="block"))

    def test_i4_extra_cannot_clobber_the_event_discriminator_or_session_id(self) -> None:
        out = _codec().encode_input(
            Input(
                event="PreToolUse",
                session_id="real-session",
                cwd="/work",
                tool_name="Bash",
                tool_input={"command": "echo hi"},
                extra={"hook_event_name": "Bogus", "session_id": "fake-session"},
            )
        )
        # Gemini's native name for PreToolUse is BeforeTool, from the
        # capability map, never a hard-coded literal.
        assert out["hook_event_name"] == "BeforeTool"
        assert out["session_id"] == "real-session"

    # Invariant 1: native-wins decode. Gemini lists host event SessionEnd
    # twice -- native -> SessionEnd, close -> Stop -- and only the native
    # row is what Gemini actually emits.
    def test_i1_session_end_decodes_to_canonical_session_end(self) -> None:
        inp = _codec().decode_input({"hook_event_name": "SessionEnd", "session_id": "s", "cwd": "/tmp"})
        assert inp.event == "SessionEnd"

    def test_i5_decode_decision_falls_back_to_exit_code(self) -> None:
        assert _codec().decode_decision("", 2).action == "block"
        assert _codec().decode_decision("", 0).action == "allow"

    def test_i5_recognized_shape_gate_empty_object_at_block_exit_falls_back_to_block(self) -> None:
        assert _codec().decode_decision({}, 2).action == "block"

    def test_i5_recognized_shape_gate_unrecognized_json_at_block_exit_falls_back_to_block(self) -> None:
        assert _codec().decode_decision({"unrelated": 1}, 2).action == "block"

    def test_i5_recognized_shape_wins_over_a_disagreeing_exit_code(self) -> None:
        assert _codec().decode_decision({"decision": "deny", "reason": "no"}, 0).action == "block"

    # Invariant 6: gemini does not vary wire shape by event -- a blank
    # event is correct.
    def test_i6_encode_decision_produces_the_same_shape_regardless_of_event(self) -> None:
        blank = _codec().encode_decision(Decision(action="allow"))
        with_event = _codec().encode_decision(Decision(event="PreToolUse", action="allow"))
        assert blank == with_event

    def test_regression_explicit_empty_event_is_accepted_like_omitted(self) -> None:
        with_empty = _codec().encode_decision(Decision(event="", action="allow"))
        omitted = _codec().encode_decision(Decision(action="allow"))
        assert with_empty == omitted

    def test_i6_decode_decision_never_sets_event(self) -> None:
        decision = _codec().decode_decision(fixture("gemini", "PreToolUse.block.json"), 2)
        assert decision.event == ""
        assert decision.action == "block"
        assert decision.message == "blocked by policy"

    # No PreToolUse.rewrite fixture exists on disk for gemini (rewrite is
    # a real decode path -- hookSpecificOutput.tool_input -- but has never
    # been captured from the host), so this pins it against a literal
    # payload shaped the way encode_decision's own rewrite branch
    # produces it.
    def test_decode_decision_carries_rewrite_payload_from_tool_input(self) -> None:
        decision = _codec().decode_decision(
            {"decision": "allow", "hookSpecificOutput": {"tool_input": {"command": "echo safe"}}},
            0,
        )
        assert decision.action == "rewrite"
        assert decision.rewrite == {"command": "echo safe"}

    # Invariant 7 does not apply: gemini has no SessionStart context shape.
    def test_i7_na_for_gemini_session_start_allow_decodes_to_plain_allow(self) -> None:
        decision = _codec().decode_decision(fixture("gemini", "SessionStart.allow.json"), 0)
        assert decision.action == "allow"
        assert (decision.metadata or {}) == {}

    def test_i8_na_for_gemini_gemini_does_have_an_exit_code_contract_for_block(self) -> None:
        _stdout, exit_code = _codec().encode_decision(Decision(action="block", message="no"))
        assert exit_code == 2

    def test_warn_fold_pre_tool_use_warn_fixture_decodes_to_warn_via_system_message(self) -> None:
        decision = _codec().decode_decision(fixture("gemini", "PreToolUse.warn.json"), 0)
        assert decision.action == "warn"
        assert decision.message == "consider a safer flag"

    def test_warn_fold_same_reason_without_system_message_decodes_to_allow(self) -> None:
        decision = _codec().decode_decision({"decision": "allow", "reason": "fyi"}, 0)
        assert decision.action == "allow"
