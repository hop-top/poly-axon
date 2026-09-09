"""The OpenCode codec.

OpenCode hooks are in-process JS/TS plugin callbacks, not subprocesses:
there is no exit-code contract (spec/hosts/opencode/host.yaml has no
exit_codes key). encode_decision always returns exit 0; decode_decision
ignores its exit argument and decodes the decision from stdout JSON only
(invariant 8).

Event names are never hardcoded here: they come from
capabilities().host_event for encoding and the native-wins decode map for
decoding, so spec/hosts/opencode/capabilities.yaml is the single source
of the canonical<->dotted event mapping.

Mirrors hooks/hosts/opencode/codec.go; matched against
ts/src/hooks/hosts/opencode.ts for shape.
"""

from __future__ import annotations

from axon.capabilities import Capabilities, host_event as cap_host_event, level, load_capabilities
from axon.errors import SchemaError, UnsupportedActionError, UnsupportedEventError
from axon.hooks.decision import Decision
from axon.hooks.input import Input

_KNOWN_INPUT_KEYS = {"type", "session_id", "tool", "input", "output", "prompt"}


class OpencodeCodec:
    def __init__(self) -> None:
        self._caps = load_capabilities("opencode")
        self._rev = _decode_map(self._caps)

    def host(self) -> str:
        return "opencode"

    def capabilities(self) -> Capabilities:
        return self._caps

    def encode_input(self, inp: Input) -> dict:
        native = cap_host_event(self._caps, inp.event)
        if native is None:
            raise UnsupportedEventError("opencode", inp.event)

        out: dict = dict(inp.extra or {})
        out["type"] = native
        out["session_id"] = inp.session_id
        if inp.event in ("PreToolUse", "PermissionRequest"):
            out["tool"] = inp.tool_name
            out["input"] = inp.tool_input
        elif inp.event == "PostToolUse":
            out["tool"] = inp.tool_name
            out["input"] = inp.tool_input
            out["output"] = inp.tool_response
        elif inp.event == "UserPromptSubmit":
            out["prompt"] = inp.prompt
        return out

    def decode_input(self, raw: dict) -> Input:
        m = _as_dict(raw, "opencode input")
        native = m.get("type")
        if not isinstance(native, str) or native == "":
            # A missing discriminator is a malformed payload, not an
            # unsupported event: reporting it as "opencode/" named no
            # event at all.
            raise SchemaError("opencode input: missing type")
        event = self._rev.get(native)
        if event is None:
            raise UnsupportedEventError("opencode", native)

        extra = {k: v for k, v in m.items() if k not in _KNOWN_INPUT_KEYS}

        inp = Input(
            event=event,
            session_id=m.get("session_id") if isinstance(m.get("session_id"), str) else "",
            cwd="",
            extra=extra,
        )
        if isinstance(m.get("tool"), str):
            inp.tool_name = m["tool"]
        if isinstance(m.get("input"), dict):
            inp.tool_input = m["input"]
        if isinstance(m.get("output"), dict):
            inp.tool_response = m["output"]
        if isinstance(m.get("prompt"), str):
            inp.prompt = m["prompt"]
        return inp

    # encode_decision always returns exit 0 (invariant 8). Warn is passed
    # through verbatim as {"action":"warn","message":...} -- folding it
    # to block would change semantics (block a tool the handler only
    # warned about), so it is never done (invariant 2).
    def encode_decision(self, d: Decision) -> tuple[object, int]:
        self._check_event(d.event)
        if d.action == "allow":
            return {"action": "allow"}, 0
        if d.action == "rewrite":
            # Field name "rewrite" per xat
            # spec/hook-output/opencode/tool.execute.before-rewrite.json.
            return {"action": "rewrite", "rewrite": d.rewrite}, 0
        if d.action == "warn":
            return {"action": "warn", "message": d.message or ""}, 0
        if d.action == "block":
            return {"action": "block", "message": d.message or ""}, 0
        raise UnsupportedActionError(f"opencode cannot express action {d.action!r}")

    # decode_decision ignores exit entirely: OpenCode decisions live in
    # stdout JSON only. Empty stdout decodes to allow.
    def decode_decision(self, stdout: object, exit_code: int) -> Decision:
        if _is_empty_stdout(stdout):
            return Decision(action="allow")
        m = _as_dict(stdout, "opencode decision")
        d = Decision(action="allow")
        action = m.get("action")
        if action == "block":
            d.action = "block"
        elif action == "warn":
            d.action = "warn"
        elif action == "rewrite":
            d.action = "rewrite"
            d.rewrite = m.get("rewrite")
        message = m.get("message")
        d.message = message if isinstance(message, str) else ""
        return d

    # _check_event rejects a decision naming an event OpenCode has no
    # wire shape for. A blank event keeps the host's default shape.
    def _check_event(self, ev: str) -> None:
        if not ev:
            return
        lvl, ok = level(self._caps, ev)
        if not ok or lvl not in ("native", "close"):
            raise UnsupportedEventError("opencode", ev)


# _decode_map: native rows always win over close rows. A close row whose
# host event no native row claims is unambiguous and does decode
# (todo.updated -> TaskCompleted and tui.prompt.append ->
# UserPromptSubmit are the only way those envelopes decode at all).
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


def make_codec() -> OpencodeCodec:
    return OpencodeCodec()
