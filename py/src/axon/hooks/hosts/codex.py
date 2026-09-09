"""The Codex CLI codec. Codex's handler contract mirrors Claude Code's:
stdin JSON envelope, stdout JSON decision, exit code. Codex's host-side
event names also equal the canonical names.

Mirrors hooks/hosts/codex/codec.go; matched against
ts/src/hooks/hosts/codex.ts for shape.
"""

from __future__ import annotations

from axon.capabilities import Capabilities, level, load_capabilities
from axon.errors import SchemaError, UnsupportedActionError, UnsupportedEventError
from axon.hooks.decision import Decision
from axon.hooks.input import Input
from axon.registry import get

_KNOWN_INPUT_KEYS = {"hook_event_name", "session_id", "cwd", "tool_name", "tool_input", "tool_response", "prompt"}

_NATIVE_ACTION = {"allow": "allow", "warn": "ask", "block": "deny"}


class CodexCodec:
    def __init__(self) -> None:
        self._caps = load_capabilities("codex")
        self._host_to_canonical = _decode_map(self._caps)

    def host(self) -> str:
        return "codex"

    def capabilities(self) -> Capabilities:
        return self._caps

    def encode_input(self, inp: Input) -> dict:
        _require_native_or_close(self._caps, "codex", inp.event)

        out: dict = dict(inp.extra or {})
        out["hook_event_name"] = inp.event
        out["session_id"] = inp.session_id
        out["cwd"] = inp.cwd
        if inp.event == "PreToolUse":
            out["tool_name"] = inp.tool_name
            out["tool_input"] = inp.tool_input
        elif inp.event == "PostToolUse":
            out["tool_name"] = inp.tool_name
            out["tool_input"] = inp.tool_input
            out["tool_response"] = inp.tool_response
        elif inp.event == "UserPromptSubmit":
            out["prompt"] = inp.prompt
        return out

    def decode_input(self, raw: dict) -> Input:
        m = _as_dict(raw, "codex input")
        host_event_name = m.get("hook_event_name")
        if not isinstance(host_event_name, str) or host_event_name == "":
            raise SchemaError("codex input: missing hook_event_name")
        event = self._host_to_canonical.get(host_event_name)
        if event is None:
            raise UnsupportedEventError("codex", host_event_name)

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

    # encode_decision mirrors claude's shape switch: a blank event or
    # PreToolUse use the tool-gate shape; PostToolUse uses the top-level
    # decision/reason shape; SessionStart allows with the empty envelope
    # (or context metadata) and blocks with the same top-level shape.
    # UserPromptSubmit and Stop allow with the empty envelope; codex
    # defines no dedicated block shape for either, so block on those two
    # is unsupported, same as claude.
    def encode_decision(self, d: Decision) -> tuple[object, int]:
        if d.event:
            _require_native_or_close(self._caps, "codex", d.event)
        exit_code = _exit_for(self._caps, d.action)
        event = d.event or "PreToolUse"
        if event == "PreToolUse":
            return _encode_pre_tool_use_decision(d), exit_code
        if event == "PostToolUse":
            return _encode_generic_block_decision(d, "PostToolUse"), exit_code
        if event == "SessionStart":
            return _encode_session_start_decision(d), exit_code
        if event in ("UserPromptSubmit", "Stop"):
            if d.action in ("allow", "warn"):
                return {}, exit_code
            raise UnsupportedActionError(f"codex decision {d.action} for {event}")
        raise UnsupportedEventError("codex", str(d.event))

    def decode_decision(self, stdout: object, exit_code: int) -> Decision:
        if _is_empty_stdout(stdout):
            return Decision(action=_action_for_exit(self._caps, exit_code))
        m = _as_dict(stdout, "codex decision")
        decision, recognized = _decode_known_shape(m)
        if not recognized:
            return Decision(action=_action_for_exit(self._caps, exit_code))
        return decision


def _require_native_or_close(caps: Capabilities, host: str, event: str) -> None:
    lvl, ok = level(caps, event)
    if not ok or lvl not in ("native", "close"):
        raise UnsupportedEventError(host, event)


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
            continue
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
            raise UnsupportedActionError(f"codex cannot express action {d.action!r}")
        return {
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": native,
                "permissionDecisionReason": d.message or "",
            }
        }
    raise UnsupportedActionError(f"codex cannot express action {d.action!r}")


def _encode_generic_block_decision(d: Decision, event: str) -> dict:
    if d.action in ("allow", "warn"):
        return {}
    if d.action == "block":
        return {"decision": "block", "reason": d.message or ""}
    raise UnsupportedActionError(f"codex decision {d.action} for {event}")


def _encode_session_start_decision(d: Decision) -> dict:
    if d.action in ("allow", "warn"):
        with_context = _encode_session_start_context(d.metadata)
        return with_context if with_context is not None else {}
    if d.action == "block":
        return {"decision": "block", "reason": d.message or ""}
    raise UnsupportedActionError(f"codex decision {d.action} for SessionStart")


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


# _decode_known_shape mirrors claude's, minus the PermissionRequest
# branch: codex has no PermissionRequest decision channel.
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


def _exit_for(caps: Capabilities, action: str) -> int:
    codes = _host_exit_codes(caps)
    if action == "allow":
        return codes.allow
    if action == "warn":
        return codes.warn if codes.warn is not None else codes.allow
    if action == "block":
        return codes.block
    if action == "rewrite":
        return codes.rewrite if codes.rewrite is not None else codes.allow
    return codes.allow


# _action_for_exit: codex's host.yaml (like claude's) leaves warn unset,
# defaulting it to the same value as allow, so an unset warn code can
# never win a match here unless it is explicitly declared and distinct.
def _action_for_exit(caps: Capabilities, exit_code: int) -> str:
    codes = _host_exit_codes(caps)
    if exit_code == codes.block:
        return "block"
    if codes.rewrite is not None and exit_code == codes.rewrite:
        return "rewrite"
    if codes.warn is not None and codes.warn != codes.allow and exit_code == codes.warn:
        return "warn"
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


def make_codec() -> CodexCodec:
    return CodexCodec()
