"""Pins the eight task invariants against the claude codec. Matched
against hooks/hosts/claude/codec.go and hooks/hosts/claude/codec_test.go.
"""

from __future__ import annotations

import pytest

from axon import UnsupportedActionError, UnsupportedEventError, validate
from axon.hooks import Decision, Input
from axon.hooks import codec_for
from axon.hooks.hosts import all as _all  # noqa: F401

from .testutil import fixture


def _codec():
    c = codec_for("claude")
    assert c is not None, "claude codec not registered"
    return c


class TestClaudeCodec:
    def test_host_returns_the_canonical_name(self) -> None:
        assert _codec().host() == "claude"

    # Invariant 2: unknown/empty action on encode raises UnsupportedActionError.
    def test_i2_encode_decision_raises_for_an_unrecognized_action(self) -> None:
        with pytest.raises(UnsupportedActionError):
            _codec().encode_decision(Decision(action="bogus"))  # type: ignore[arg-type]

    def test_i2_encode_decision_never_maps_warn_to_block_on_pre_tool_use(self) -> None:
        stdout, _exit = _codec().encode_decision(Decision(action="warn"))
        hso = stdout.get("hookSpecificOutput") if isinstance(stdout, dict) else None
        assert hso is not None
        assert hso.get("permissionDecision") == "ask"
        assert hso.get("permissionDecision") != "deny"

    # Invariant 3: encode_decision rejects an event the capability map does
    # not mark native/close.
    def test_i3_encode_decision_raises_for_an_event_claude_does_not_classify(self) -> None:
        with pytest.raises(UnsupportedEventError):
            _codec().encode_decision(Decision(event="TeammateIdle", action="block"))

    def test_i3_encode_decision_accepts_a_blank_event(self) -> None:
        _codec().encode_decision(Decision(action="allow"))  # must not raise

    # Regression: Go's `switch d.Event { case "", axon.EventPreToolUse: ... }`
    # treats an explicit empty string exactly like an omitted field -- both
    # must produce the identical PreToolUse shape, never UnsupportedEventError.
    def test_regression_explicit_empty_event_behaves_like_omitted(self) -> None:
        with_empty = _codec().encode_decision(Decision(event="", action="block", message="no"))
        omitted = _codec().encode_decision(Decision(action="block", message="no"))
        assert with_empty == omitted
        hso = with_empty[0]["hookSpecificOutput"]
        assert hso["permissionDecision"] == "deny"

    # Invariant 4: extra never overwrites a canonical envelope field.
    def test_i4_extra_cannot_clobber_the_event_discriminator_or_session_id(self) -> None:
        out = _codec().encode_input(
            Input(
                event="PreToolUse",
                session_id="real-session",
                cwd="/work",
                tool_name="Bash",
                tool_input={"command": "echo hi"},
                extra={"hook_event_name": "Bogus", "session_id": "fake-session", "cwd": "/nope"},
            )
        )
        assert out["hook_event_name"] == "PreToolUse"
        assert out["session_id"] == "real-session"
        assert out["cwd"] == "/work"

    # Invariant 1: native-wins decode. Claude's capability file is
    # all-native, so this is exercised generically.
    def test_i1_pre_tool_use_input_fixture_decodes_to_pre_tool_use(self) -> None:
        raw = fixture("claude", "PreToolUse.input.json")
        inp = _codec().decode_input(raw)
        assert inp.event == "PreToolUse"

    # Invariant 5: decode_decision falls back to exit code on empty
    # stdout, on {}, and on unrecognized JSON -- all three, each with a
    # block exit.
    def test_i5_decode_decision_falls_back_to_exit_code_on_empty_stdout(self) -> None:
        decision = _codec().decode_decision("", 2)
        assert decision.action == "block"

    def test_i5_decode_decision_falls_back_to_exit_code_on_empty_object(self) -> None:
        decision = _codec().decode_decision({}, 2)
        assert decision.action == "block"

    def test_i5_decode_decision_falls_back_to_exit_code_on_unrecognized_json(self) -> None:
        decision = _codec().decode_decision({"unrelated": 1}, 2)
        assert decision.action == "block"

    def test_i5_decode_decision_falls_back_to_allow_on_empty_stdout_with_exit_zero(self) -> None:
        decision = _codec().decode_decision("", 0)
        assert decision.action == "allow"

    # Invariant 6: wire shape varies by event; decision.event selects the
    # shape on encode and is set on decode when the wire reveals it.
    def test_i6_encode_decision_picks_permission_request_shape(self) -> None:
        stdout, _exit = _codec().encode_decision(Decision(event="PermissionRequest", action="block", message="no"))
        decision = stdout["hookSpecificOutput"]["decision"]
        assert decision["behavior"] == "deny"
        assert decision["message"] == "no"

    def test_i6_encode_decision_picks_post_tool_use_shape(self) -> None:
        stdout, _exit = _codec().encode_decision(
            Decision(event="PostToolUse", action="block", message="post-check failed")
        )
        assert stdout == {"decision": "block", "reason": "post-check failed"}

    def test_i6_decode_decision_sets_event_from_hook_specific_output(self) -> None:
        decision = _codec().decode_decision(fixture("claude", "PermissionRequest.block.json"), 0)
        assert decision.event == "PermissionRequest"
        assert decision.message == "blocked by policy"

    # decode_decision must carry the wire's message/rewrite payload
    # through, not just classify the action -- test_fixtures.py's
    # round-trip only asserts .action (per contract notes S6), so these
    # fields need their own assertion here or a dropped field slips past
    # every test.
    def test_decode_decision_carries_pre_tool_use_block_message(self) -> None:
        decision = _codec().decode_decision(fixture("claude", "PreToolUse.block.json"), 2)
        assert decision.action == "block"
        assert decision.message == "blocked by policy"

    def test_decode_decision_carries_pre_tool_use_rewrite_payload(self) -> None:
        decision = _codec().decode_decision(fixture("claude", "PreToolUse.rewrite.json"), 3)
        assert decision.action == "rewrite"
        assert decision.rewrite == {"command": "echo hello --safe"}

    def test_decode_decision_carries_post_tool_use_block_message(self) -> None:
        decision = _codec().decode_decision(fixture("claude", "PostToolUse.block.json"), 2)
        assert decision.action == "block"
        assert decision.message == "post-check failed"

    # Invariant 7: claude SessionStart allow decisions carry context
    # through metadata; a contextless allow is exactly {}.
    def test_i7_contextless_session_start_allow_encodes_to_exactly_empty(self) -> None:
        stdout, _exit = _codec().encode_decision(Decision(event="SessionStart", action="allow"))
        assert stdout == {}

    def test_i7_session_start_allow_fixture_round_trips_through_metadata_byte_equal(self) -> None:
        raw = fixture("claude", "SessionStart.allow.json")
        decision = _codec().decode_decision(raw, 0)
        assert decision.action == "allow"
        assert decision.event == "SessionStart"
        assert decision.metadata == {
            "additional_context": "loaded project context",
            "system_message": "session ready",
            "continue": True,
        }
        decision.event = "SessionStart"
        stdout, _exit = _codec().encode_decision(decision)
        assert stdout == raw
        validate("claude/SessionStart.allow.json", "hosts/claude/hooks/SessionStart.allow.schema.json", stdout)

    # Invariant 8 does not apply to claude (it has an exit-code contract).
    def test_i8_na_for_claude_claude_does_have_an_exit_code_contract(self) -> None:
        _stdout, exit_code = _codec().encode_decision(Decision(action="block"))
        assert exit_code == 2
