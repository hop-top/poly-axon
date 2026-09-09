"""Pins the eight task invariants against the codex codec. Matched
against hooks/hosts/codex/codec.go.
"""

from __future__ import annotations

import pytest

from axon import UnsupportedActionError, UnsupportedEventError, validate
from axon.hooks import Decision, Input
from axon.hooks import codec_for
from axon.hooks.hosts import all as _all  # noqa: F401

from .testutil import fixture


def _codec():
    c = codec_for("codex")
    assert c is not None, "codex codec not registered"
    return c


class TestCodexCodec:
    def test_host_returns_the_canonical_name(self) -> None:
        assert _codec().host() == "codex"

    def test_i2_encode_decision_raises_for_an_unrecognized_action(self) -> None:
        with pytest.raises(UnsupportedActionError):
            _codec().encode_decision(Decision(action="bogus"))  # type: ignore[arg-type]

    def test_i2_encode_decision_never_maps_warn_to_block_on_pre_tool_use(self) -> None:
        stdout, _exit = _codec().encode_decision(Decision(action="warn"))
        hso = stdout["hookSpecificOutput"]
        assert hso["permissionDecision"] == "ask"
        assert hso["permissionDecision"] != "deny"

    def test_i3_encode_decision_raises_for_an_event_codex_does_not_classify(self) -> None:
        with pytest.raises(UnsupportedEventError):
            _codec().encode_decision(Decision(event="TeammateIdle", action="block"))

    def test_i3_encode_decision_accepts_a_blank_event(self) -> None:
        _codec().encode_decision(Decision(action="allow"))  # must not raise

    def test_regression_explicit_empty_event_behaves_like_omitted(self) -> None:
        with_empty = _codec().encode_decision(Decision(event="", action="block", message="no"))
        omitted = _codec().encode_decision(Decision(action="block", message="no"))
        assert with_empty == omitted
        hso = with_empty[0]["hookSpecificOutput"]
        assert hso["permissionDecision"] == "deny"

    def test_i4_extra_cannot_clobber_the_event_discriminator_or_session_id(self) -> None:
        out = _codec().encode_input(
            Input(
                event="PreToolUse",
                session_id="real-session",
                cwd="/work",
                tool_name="shell",
                tool_input={"command": "echo hi"},
                extra={"hook_event_name": "Bogus", "session_id": "fake-session"},
            )
        )
        assert out["hook_event_name"] == "PreToolUse"
        assert out["session_id"] == "real-session"

    def test_i1_pre_tool_use_input_fixture_decodes_to_pre_tool_use(self) -> None:
        inp = _codec().decode_input(fixture("codex", "PreToolUse.input.json"))
        assert inp.event == "PreToolUse"

    def test_i5_decode_decision_falls_back_to_exit_code_on_empty_stdout(self) -> None:
        assert _codec().decode_decision("", 2).action == "block"

    def test_i5_decode_decision_falls_back_to_exit_code_on_empty_object(self) -> None:
        assert _codec().decode_decision({}, 2).action == "block"

    def test_i5_decode_decision_falls_back_to_exit_code_on_unrecognized_json(self) -> None:
        assert _codec().decode_decision({"unrelated": 1}, 2).action == "block"

    def test_decode_decision_carries_pre_tool_use_block_message(self) -> None:
        decision = _codec().decode_decision(fixture("codex", "PreToolUse.block.json"), 2)
        assert decision.action == "block"
        assert decision.message == "blocked by policy"

    def test_decode_decision_carries_pre_tool_use_rewrite_payload(self) -> None:
        decision = _codec().decode_decision(fixture("codex", "PreToolUse.rewrite.json"), 3)
        assert decision.action == "rewrite"
        assert decision.rewrite == {"command": "echo hello --safe"}

    def test_decode_decision_carries_post_tool_use_block_message(self) -> None:
        decision = _codec().decode_decision(fixture("codex", "PostToolUse.block.json"), 2)
        assert decision.action == "block"
        assert decision.message == "post-condition failed"

    def test_i6_encode_decision_picks_post_tool_use_shape(self) -> None:
        stdout, _exit = _codec().encode_decision(
            Decision(event="PostToolUse", action="block", message="post-condition failed")
        )
        assert stdout == {"decision": "block", "reason": "post-condition failed"}

    def test_i6_user_prompt_submit_and_stop_allow_with_empty_envelope(self) -> None:
        for event in ("UserPromptSubmit", "Stop"):
            stdout, _exit = _codec().encode_decision(Decision(event=event, action="allow"))
            assert stdout == {}

    def test_i6_user_prompt_submit_block_is_unsupported(self) -> None:
        with pytest.raises(UnsupportedActionError):
            _codec().encode_decision(Decision(event="UserPromptSubmit", action="block"))

    def test_i7_contextless_session_start_allow_encodes_to_exactly_empty(self) -> None:
        stdout, _exit = _codec().encode_decision(Decision(event="SessionStart", action="allow"))
        assert stdout == {}

    def test_i7_session_start_allow_fixture_round_trips_through_metadata_byte_equal(self) -> None:
        raw = fixture("codex", "SessionStart.allow.json")
        decision = _codec().decode_decision(raw, 0)
        assert decision.action == "allow"
        assert decision.event == "SessionStart"
        assert decision.metadata == {
            "additional_context": "session initialized",
            "continue": True,
        }
        decision.event = "SessionStart"
        stdout, _exit = _codec().encode_decision(decision)
        assert stdout == raw
        validate("codex/SessionStart.allow.json", "hosts/codex/hooks/SessionStart.allow.schema.json", stdout)

    def test_i8_na_for_codex_codex_does_have_an_exit_code_contract(self) -> None:
        _stdout, exit_code = _codec().encode_decision(Decision(action="block"))
        assert exit_code == 2
