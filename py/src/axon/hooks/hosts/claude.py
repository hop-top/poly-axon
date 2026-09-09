"""The Claude Code codec. Claude's host-side event names equal the
canonical names (its capabilities.yaml is all-native, identity mapping),
but the decode map is still built and consulted via the capability reader
rather than trusted verbatim -- an unrecognized hook_event_name is
UnsupportedEventError, never a canonical Event carrying whatever string
the wire held.

Mirrors hooks/hosts/claude/codec.go; matched against
ts/src/hooks/hosts/claude.ts for shape.
"""

from __future__ import annotations

from axon.capabilities import Capabilities, level, load_capabilities
from axon.errors import SchemaError, UnsupportedActionError, UnsupportedEventError
from axon.hooks.decision import Decision
from axon.hooks.input import Input
from axon.registry import get

_KNOWN_INPUT_KEYS = {"hook_event_name", "session_id", "cwd", "tool_name", "tool_input", "tool_response", "prompt"}

# nativeAction maps canonical actions to Claude's PreToolUse
# permissionDecision. warn has no distinct exit code in Claude's hook
# contract; it still surfaces as the native "ask" permissionDecision.
_NATIVE_ACTION = {"allow": "allow", "warn": "ask", "block": "deny"}


class ClaudeCodec:
    def __init__(self) -> None:
        self._caps = load_capabilities("claude")
        self._host_to_canonical = _decode_map(self._caps)

    def host(self) -> str:
        return "claude"

    def capabilities(self) -> Capabilities:
        return self._caps

    # encode_input builds Claude's native hook stdin envelope. Requires
    # the event be native or close: a synthesized event has no envelope
    # of its own (it is derived from some other host event by the
    # runtime).
    def encode_input(self, inp: Input) -> dict:
        _require_native_or_close(self._caps, "claude", inp.event)

        # extra is written FIRST; the canonical fields below overwrite
        # any colliding key, so a colliding extra key can never clobber
        # the envelope's own discriminator or session id (invariant 4).
        out: dict = dict(inp.extra or {})
        out["hook_event_name"] = inp.event
        out["session_id"] = inp.session_id
        out["cwd"] = inp.cwd
        if inp.event in ("PreToolUse", "PermissionRequest"):
            out["tool_name"] = inp.tool_name
            out["tool_input"] = inp.tool_input
        elif inp.event == "PostToolUse":
            out["tool_name"] = inp.tool_name
            out["tool_input"] = inp.tool_input
            out["tool_response"] = inp.tool_response
        elif inp.event == "UserPromptSubmit":
            out["prompt"] = inp.prompt
        return out

    # decode_input parses Claude's native hook stdin envelope. The event
    # is looked up in the capability map rather than trusted verbatim.
    def decode_input(self, raw: dict) -> Input:
        m = _as_dict(raw, "claude input")
        host_event_name = m.get("hook_event_name")
        if not isinstance(host_event_name, str) or host_event_name == "":
            raise SchemaError("claude input: missing hook_event_name")
        event = self._host_to_canonical.get(host_event_name)
        if event is None:
            raise UnsupportedEventError("claude", host_event_name)

        extra = {k: v for k, v in m.items() if k not in _KNOWN_INPUT_KEYS}

        inp = Input(
            event=event,
            session_id=m.get("session_id") if isinstance(m.get("session_id"), str) else "",
            cwd=m.get("cwd") if isinstance(m.get("cwd"), str) else "",
            extra=extra,
        )
        if isinstance(m.get("tool_name"), str):
            inp.tool_name = m["tool_name"]
        if isinstance(m.get("tool_input"), dict):
            inp.tool_input = m["tool_input"]
        if isinstance(m.get("tool_response"), dict):
            inp.tool_response = m["tool_response"]
        if isinstance(m.get("prompt"), str):
            inp.prompt = m["prompt"]
        return inp

    # encode_decision picks the wire shape by decision.event: a blank
    # event or PreToolUse use the tool-gate
    # hookSpecificOutput.permissionDecision shape; PermissionRequest,
    # PostToolUse and SessionStart each have their own native shape. Any
    # other event has no decision channel in Claude's hook contract.
    def encode_decision(self, d: Decision) -> tuple[object, int]:
        if d.event:
            _require_native_or_close(self._caps, "claude", d.event)
        exit_code = _exit_for(self._caps, d.action)
        # A blank event (omitted or explicit "") behaves identically to
        # PreToolUse, matching Go's `switch d.Event { case "",
        # axon.EventPreToolUse: ... }`.
        event = d.event or "PreToolUse"
        if event == "PreToolUse":
            return _encode_pre_tool_use_decision(d), exit_code
        if event == "PermissionRequest":
            return _encode_permission_request_decision(d), exit_code
        if event == "PostToolUse":
            return _encode_generic_block_decision(d, "PostToolUse"), exit_code
        if event == "SessionStart":
            return _encode_session_start_decision(d), exit_code
        raise UnsupportedEventError("claude", str(d.event))

    # decode_decision classifies stdout by its recognized decision shape.
    # Empty stdout, or JSON carrying none of Claude's known decision
    # keys, both fall back to _action_for_exit(exit): the exit code is
    # the only remaining signal (invariant 5).
    def decode_decision(self, stdout: object, exit_code: int) -> Decision:
        if _is_empty_stdout(stdout):
            return Decision(action=_action_for_exit(self._caps, exit_code))
        m = _as_dict(stdout, "claude decision")
        decision, recognized = _decode_known_shape(m)
        if not recognized:
            return Decision(action=_action_for_exit(self._caps, exit_code))
        return decision


# _require_native_or_close enforces that encode_input/encode_decision only
# produce a payload the host itself would emit: a synthesized or
# unclassified event has no envelope/decision channel of its own.
def _require_native_or_close(caps: Capabilities, host: str, event: str) -> None:
    lvl, ok = level(caps, event)
    if not ok or lvl not in ("native", "close"):
        raise UnsupportedEventError(host, event)


# _decode_map builds the host-event -> canonical-event map used to decode
# a native stdin envelope. Native rows always win over close rows
# (invariant 1); two rows within one section sharing a host event is an
# error, never a silent last-row-wins.
def _decode_map(caps: Capabilities) -> dict[str, str]:
    m: dict[str, str] = {}
    for p in caps.native:
        if p.host_event in m:
            raise SchemaError(
                f"{caps.host}: host event {p.host_event} maps to both {m[p.host_event]} and {p.event} under native"
            )
        m[p.host_event] = p.event
    seen_close: dict[str, str] = {}
    for p in caps.close or []:
        if p.host_event in seen_close:
            raise SchemaError(
                f"{caps.host}: host event {p.host_event} maps to both "
                f"{seen_close[p.host_event]} and {p.event} under close"
            )
        seen_close[p.host_event] = p.event
        if p.host_event in m:
            continue  # native wins; the close row stays encode-only
        m[p.host_event] = p.event
    return m


def _encode_pre_tool_use_decision(d: Decision) -> dict:
    if d.action == "allow":
        return {}
    if d.action == "rewrite":
        return {
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": "allow",
                "updatedInput": d.rewrite,
            }
        }
    if d.action in ("block", "warn"):
        native = _NATIVE_ACTION.get(d.action)
        if native is None:
            raise UnsupportedActionError(f"claude cannot express action {d.action!r}")
        return {
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": native,
                "permissionDecisionReason": d.message or "",
            }
        }
    raise UnsupportedActionError(f"claude cannot express action {d.action!r}")


# _encode_permission_request_decision: warn has no native channel on this
# event, so it degrades to allow, matching nerv's warn-degrades-to-allow
# rule for PermissionRequest specifically (never to block).
def _encode_permission_request_decision(d: Decision) -> dict:
    if d.action in ("allow", "warn"):
        return {}
    if d.action == "block":
        return {
            "hookSpecificOutput": {
                "hookEventName": "PermissionRequest",
                "decision": {"behavior": "deny", "message": d.message or ""},
            }
        }
    if d.action == "rewrite":
        return {
            "hookSpecificOutput": {
                "hookEventName": "PermissionRequest",
                "decision": {"behavior": "allow", "updatedInput": d.rewrite},
            }
        }
    raise UnsupportedActionError(f"claude decision {d.action} for PermissionRequest")


# _encode_generic_block_decision is the top-level decision/reason shape
# shared by PostToolUse and (contextless) SessionStart block. Neither has
# a rewrite channel.
def _encode_generic_block_decision(d: Decision, event: str) -> dict:
    if d.action in ("allow", "warn"):
        return {}
    if d.action == "block":
        return {"decision": "block", "reason": d.message or ""}
    raise UnsupportedActionError(f"claude decision {d.action} for {event}")


# _encode_session_start_decision: a contextless allow is the empty
# envelope. An allow carrying context in metadata emits Claude's native
# additionalContext/systemMessage/continue shape instead (invariant 7).
def _encode_session_start_decision(d: Decision) -> dict:
    if d.action in ("allow", "warn"):
        with_context = _encode_session_start_context(d.metadata)
        return with_context if with_context is not None else {}
    if d.action == "block":
        return {"decision": "block", "reason": d.message or ""}
    raise UnsupportedActionError(f"claude decision {d.action} for SessionStart")


def _encode_session_start_context(metadata: dict | None) -> dict | None:
    if not metadata:
        return None
    out: dict = {}
    additional_context = metadata.get("additional_context")
    if isinstance(additional_context, str) and additional_context != "":
        out["hookSpecificOutput"] = {"additionalContext": additional_context}
    system_message = metadata.get("system_message")
    if isinstance(system_message, str) and system_message != "":
        out["systemMessage"] = system_message
    if isinstance(metadata.get("continue"), bool):
        out["continue"] = metadata["continue"]
    return out if out else None


# _decode_known_shape probes the wire map for Claude's known decision
# shapes and reports whether any were found. event is set whenever the
# wire reveals it.
def _decode_known_shape(m: dict) -> tuple[Decision, bool]:
    d = Decision(action="allow")
    recognized = False

    hso = m.get("hookSpecificOutput") if isinstance(m.get("hookSpecificOutput"), dict) else None
    if hso is not None:
        hook_event_name = hso.get("hookEventName")
        if isinstance(hook_event_name, str) and hook_event_name != "":
            d.event = hook_event_name
        if "permissionDecision" in hso:
            recognized = True
            if not d.event:
                d.event = "PreToolUse"
            pd = hso.get("permissionDecision")
            if pd == "deny":
                d.action = "block"
            elif pd == "ask":
                d.action = "warn"
            reason = hso.get("permissionDecisionReason")
            d.message = reason if isinstance(reason, str) else ""
            if "updatedInput" in hso:
                d.action = "rewrite"
                d.rewrite = hso["updatedInput"]
        decision_obj = hso.get("decision")
        if isinstance(decision_obj, dict):
            behavior = decision_obj.get("behavior")
            if isinstance(behavior, str):
                recognized = True
                d.event = "PermissionRequest"
                if behavior == "deny":
                    d.action = "block"
                    msg = decision_obj.get("message")
                    d.message = msg if isinstance(msg, str) else ""
                elif behavior == "allow":
                    if "updatedInput" in decision_obj:
                        d.action = "rewrite"
                        d.rewrite = decision_obj["updatedInput"]
                    else:
                        d.action = "allow"
        additional_context = hso.get("additionalContext")
        if isinstance(additional_context, str) and additional_context != "":
            recognized = True
            d.event = "SessionStart"
            d.action = "allow"
            _set_metadata(d, "additional_context", additional_context)

    system_message = m.get("systemMessage")
    if isinstance(system_message, str) and system_message != "":
        recognized = True
        d.event = "SessionStart"
        _set_metadata(d, "system_message", system_message)
    if isinstance(m.get("continue"), bool):
        recognized = True
        d.event = "SessionStart"
        _set_metadata(d, "continue", m["continue"])
    if m.get("decision") == "block":
        recognized = True
        d.action = "block"
        reason = m.get("reason")
        d.message = reason if isinstance(reason, str) else ""
    return d, recognized


def _set_metadata(d: Decision, key: str, value: object) -> None:
    if d.metadata is None:
        d.metadata = {}
    d.metadata[key] = value


# _exit_for maps a canonical action to Claude's process exit code,
# sourced from the host's exit_codes (never a hard-coded literal). warn
# has no distinct exit code in Claude's contract; it exits like allow.
def _exit_for(caps: Capabilities, action: str) -> int:
    codes = _host_exit_codes(caps)
    if action == "block":
        return codes.block
    if action == "rewrite":
        return codes.rewrite if codes.rewrite is not None else codes.allow
    return codes.allow


# _action_for_exit is the inverse of _exit_for, used to classify
# decisions with no recognized JSON decision shape (including empty
# stdout) by exit code alone.
def _action_for_exit(caps: Capabilities, exit_code: int) -> str:
    codes = _host_exit_codes(caps)
    if exit_code == codes.block:
        return "block"
    if codes.rewrite is not None and exit_code == codes.rewrite:
        return "rewrite"
    return "allow"


def _host_exit_codes(caps: Capabilities):
    h = _require_host(caps.host)
    if h.exit_codes is None:
        raise SchemaError(f"{caps.host}: no exit_codes in host.yaml")
    return h.exit_codes


def _require_host(name: str):
    h = get(name)
    if h is None:
        raise SchemaError(f"host {name} not found")
    return h


def _as_dict(v: object, what: str) -> dict:
    if not isinstance(v, dict):
        raise SchemaError(f"{what}: expected a JSON object")
    return v


def _is_empty_stdout(stdout: object) -> bool:
    if stdout is None or stdout == "":
        return True
    if isinstance(stdout, str):
        return stdout.strip() == ""
    return False


def make_codec() -> ClaudeCodec:
    return ClaudeCodec()
