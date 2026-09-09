"""Pins the eight task invariants against the opencode codec. Matched
against hooks/hosts/opencode/codec.go.
"""

from __future__ import annotations

import pytest

from axon import UnsupportedActionError, UnsupportedEventError
from axon.hooks import Decision, Input
from axon.hooks import codec_for
from axon.hooks.hosts import all as _all  # noqa: F401

from .testutil import fixture


def _codec():
    c = codec_for("opencode")
    assert c is not None, "opencode codec not registered"
    return c


class TestOpencodeCodec:
    def test_host_returns_the_canonical_name(self) -> None:
        assert _codec().host() == "opencode"

    def test_i2_encode_decision_raises_for_an_unrecognized_action(self) -> None:
        with pytest.raises(UnsupportedActionError):
            _codec().encode_decision(Decision(action="bogus"))  # type: ignore[arg-type]

    # OpenCode passes warn through verbatim; folding it to block would
    # change semantics (blocking a tool the handler only warned about).
    def test_i2_encode_decision_never_maps_warn_to_block(self) -> None:
        stdout, _exit = _codec().encode_decision(Decision(action="warn", message="careful"))
        assert stdout["action"] == "warn"

    def test_i3_encode_decision_raises_for_an_event_opencode_does_not_classify(self) -> None:
        with pytest.raises(UnsupportedEventError):
            _codec().encode_decision(Decision(event="TeammateIdle", action="block"))

    def test_i4_extra_cannot_clobber_the_type_discriminator_or_session_id(self) -> None:
        out = _codec().encode_input(
            Input(
                event="PreToolUse",
                session_id="real-session",
                cwd="/work",
                tool_name="Bash",
                tool_input={"command": "echo hi"},
                extra={"type": "bogus.event", "session_id": "fake-session"},
            )
        )
        assert out["type"] == "tool.execute.before"
        assert out["session_id"] == "real-session"

    # Invariant 1: native-wins decode. tui.prompt.append and todo.updated
    # are close-only rows (no native row claims them), so they still
    # decode; TaskCompleted's fixture pins that.
    def test_i1_task_completed_input_fixture_decodes_to_task_completed(self) -> None:
        inp = _codec().decode_input(fixture("opencode", "TaskCompleted.input.json"))
        assert inp.event == "TaskCompleted"

    # Invariant 8: opencode has no exit-code contract. encode_decision
    # always returns exit 0; decode_decision ignores the exit argument.
    def test_i8_encode_decision_always_returns_exit_zero(self) -> None:
        assert _codec().encode_decision(Decision(action="allow"))[1] == 0
        assert _codec().encode_decision(Decision(action="warn", message="m"))[1] == 0
        assert _codec().encode_decision(Decision(action="block", message="m"))[1] == 0
        assert _codec().encode_decision(Decision(action="rewrite", rewrite={"a": 1}))[1] == 0

    def test_i8_decode_decision_ignores_the_exit_argument_entirely(self) -> None:
        raw = fixture("opencode", "PreToolUse.block.json")
        with_zero = _codec().decode_decision(raw, 0)
        with_nonzero = _codec().decode_decision(raw, 137)
        assert with_zero == with_nonzero
        assert with_zero.action == "block"
        assert with_zero.message == "blocked by policy"

    # Both PreToolUse.block.json and PreToolUse.warn.json carry a
    # non-empty "message" on the wire; asserting only .action here would
    # pass even if decode_decision silently dropped the message.
    def test_decode_decision_carries_message_for_block(self) -> None:
        decision = _codec().decode_decision(fixture("opencode", "PreToolUse.block.json"), 0)
        assert decision.action == "block"
        assert decision.message == "blocked by policy"

    def test_decode_decision_carries_message_for_warn(self) -> None:
        decision = _codec().decode_decision(fixture("opencode", "PreToolUse.warn.json"), 0)
        assert decision.action == "warn"
        assert decision.message == "this command touches a sensitive path"

    def test_decode_decision_carries_rewrite_payload(self) -> None:
        decision = _codec().decode_decision(fixture("opencode", "PreToolUse.rewrite.json"), 0)
        assert decision.action == "rewrite"
        assert decision.rewrite == {"command": "echo safe"}

    # Invariant 5: the three fallback cases, all "with a block exit code"
    # per the brief -- opencode ignores exit entirely, so all three must
    # still decode to allow, proving the exit argument plays no role here.
    def test_i5_opencode_ignores_exit_empty_stdout_falls_back_to_allow(self) -> None:
        assert _codec().decode_decision("", 2).action == "allow"

    def test_i5_opencode_ignores_exit_empty_object_falls_back_to_allow(self) -> None:
        assert _codec().decode_decision({}, 2).action == "allow"

    def test_i5_opencode_ignores_exit_unrecognized_json_falls_back_to_allow(self) -> None:
        assert _codec().decode_decision({"unrelated": 1}, 2).action == "allow"

    # Invariant 6: opencode does not vary wire shape by event -- a blank
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
        decision = _codec().decode_decision(fixture("opencode", "PreToolUse.block.json"), 0)
        assert decision.event == ""

    def test_rewrite_field_name_is_rewrite_not_nested_under_input(self) -> None:
        stdout, _exit = _codec().encode_decision(Decision(action="rewrite", rewrite={"command": "echo safe"}))
        assert stdout == {"action": "rewrite", "rewrite": {"command": "echo safe"}}
