"""The Gemini CLI codec. Mirrors hooks/hosts/gemini/codec.go (post the
T-0026 fail-open fix: DecodeDecision now has the same recognized-shape
gate as claude and codex); matched against ts/src/hooks/hosts/gemini.ts
for shape.
"""

from __future__ import annotations

from axon.capabilities import Capabilities, host_event as cap_host_event, level, load_capabilities
from axon.errors import SchemaError, UnsupportedActionError, UnsupportedEventError
from axon.hooks.decision import Decision
from axon.hooks.input import Input
from axon.registry import get

_KNOWN_INPUT_KEYS = {"hook_event_name", "session_id", "cwd", "tool_name", "tool_input", "tool_response", "prompt"}


class GeminiCodec:
    def __init__(self) -> None:
        self._caps = load_capabilities("gemini")
        self._host_to_canonical = _decode_map(self._caps)

    def host(self) -> str:
        return "gemini"

    def capabilities(self) -> Capabilities:
        return self._caps

    # encode_input fails with UnsupportedEventError when the event has no
    # native or close mapping: synthesized, unsupported, or unclassified
    # events cannot be encoded as a Gemini-native payload since Gemini
    # itself never emits them.
    def encode_input(self, inp: Input) -> dict:
        _require_native_or_close(self._caps, "gemini", inp.event)
        host_event_name = cap_host_event(self._caps, inp.event)
        if host_event_name is None:
            raise UnsupportedEventError("gemini", inp.event)

        out: dict = dict(inp.extra or {})
        out["hook_event_name"] = host_event_name
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

    # decode_input looks up the canonical event in the native-wins decode
    # map on hook_event_name; a name not there is UnsupportedEventError,
    # never a silent fallback.
    def decode_input(self, raw: dict) -> Input:
        m = _as_dict(raw, "gemini input")
        host_event_name = m.get("hook_event_name")
        if not isinstance(host_event_name, str) or host_event_name == "":
            raise SchemaError("gemini input: missing hook_event_name")
        event = self._host_to_canonical.get(host_event_name)
        if event is None:
            raise UnsupportedEventError("gemini", host_event_name)

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

    # encode_decision emits Gemini's native decision envelope. Gemini has
    # no native "warn"; nerv folds warn into decision "allow" and, when
    # the message is non-empty, carries it in BOTH "reason" and
    # "systemMessage" (invariant 2: never mapped to block). Gemini does
    # not vary its wire shape by event (invariant 6): the event gate
    # below only rejects an event Gemini cannot express at all; it does
    # not change the shape.
    def encode_decision(self, d: Decision) -> tuple[object, int]:
        self._check_event(d.event)
        codes = _host_exit_codes(self._caps)
        if d.action == "allow":
            return {"decision": "allow"}, codes.allow
        if d.action == "warn":
            out: dict = {"decision": "allow"}
            if d.message:
                out["reason"] = d.message
                out["systemMessage"] = d.message
            # host.yaml carries no warn exit code for gemini; warn exits
            # like allow.
            return out, codes.allow
        if d.action == "rewrite":
            # host.yaml carries no rewrite exit code for gemini; rewrite
            # still allows the tool call to proceed, so it exits like
            # allow.
            return {"decision": "allow", "hookSpecificOutput": {"tool_input": d.rewrite}}, codes.allow
        if d.action == "block":
            return {"decision": "deny", "reason": d.message or ""}, codes.block
        raise UnsupportedActionError(f"gemini cannot express action {d.action!r}")

    # decode_decision reads Gemini's native decision envelope. Empty
    # stdout, or JSON that carries none of Gemini's known decision keys,
    # both fall back to _action_for_exit(exit): the exit code is the
    # only remaining signal. Gemini's wire never reveals which event a
    # decision answers, so decision.event is always left blank
    # (invariant 6).
    def decode_decision(self, stdout: object, exit_code: int) -> Decision:
        if _is_empty_stdout(stdout):
            return Decision(action=_action_for_exit(self._caps, exit_code))
        m = _as_dict(stdout, "gemini decision")
        decision, recognized = _decode_known_shape(m)
        if not recognized:
            return Decision(action=_action_for_exit(self._caps, exit_code))
        return decision

    # _check_event rejects a decision naming an event Gemini has no wire
    # shape for. A blank event keeps the default shape.
    def _check_event(self, ev: str) -> None:
        if not ev:
            return
        lvl, ok = level(self._caps, ev)
        if not ok or lvl not in ("native", "close"):
            raise UnsupportedEventError("gemini", ev)


def _require_native_or_close(caps: Capabilities, host: str, event: str) -> None:
    lvl, ok = level(caps, event)
    if not ok or lvl not in ("native", "close"):
        raise UnsupportedEventError(host, event)


# _decode_known_shape probes the wire object for Gemini's known decision
# shapes and reports whether any were found. The systemMessage-based
# warn/allow fold is preserved exactly: it disambiguates two shapes
# already recognized via "decision" rather than participating in
# recognition itself, mirroring hooks/hosts/gemini/codec.go.
def _decode_known_shape(m: dict) -> tuple[Decision, bool]:
    d = Decision(action="allow")
    recognized = False

    decision_val = m.get("decision")
    if decision_val == "deny":
        recognized = True
        d.action = "block"
    elif decision_val == "allow":
        recognized = True
        d.action = "allow"
    reason = m.get("reason")
    d.message = reason if isinstance(reason, str) else ""
    # The wire is lossy: plain allow and warn both carry
    # decision:"allow" plus "reason". Only "systemMessage" (set
    # exclusively by the Warn branch) distinguishes them; its absence
    # means this genuinely cannot be told apart from allow.
    system_message = m.get("systemMessage")
    if isinstance(system_message, str) and system_message != "":
        d.action = "warn"
    hso = m.get("hookSpecificOutput") if isinstance(m.get("hookSpecificOutput"), dict) else None
    if hso is not None and "tool_input" in hso:
        recognized = True
        d.action = "rewrite"
        d.rewrite = hso["tool_input"]
    return d, recognized


# _decode_map: native rows always win over close rows. Gemini's own
# motivating case: host event SessionEnd is listed native -> SessionEnd
# and close -> Stop; only the native row is what Gemini actually emits.
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


def _action_for_exit(caps: Capabilities, exit_code: int) -> str:
    codes = _host_exit_codes(caps)
    if exit_code == codes.block and exit_code != codes.allow:
        return "block"
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


def make_codec() -> GeminiCodec:
    return GeminiCodec()
