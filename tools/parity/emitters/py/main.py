#!/usr/bin/env python3
"""The axon parity emitter for Python. Answers one parity-harness case
against the installed axon package (py/.venv). See tools/parity/README.md
for the invocation contract this program implements; see
go/tools/parity/main.go for the reference this mirrors
operation-for-operation, and tools/parity/emitters/ts/main.mjs for the
second-language precedent.

Run through py/.venv's own interpreter (wired into parity.py's LANGUAGES
table), never bare `python3` -- `mise exec -- python3` never resolves an
activated venv (mise re-resolves its own tool on every `exec`), so this
emitter's interpreter must be the venv's own, same as make test-py.
"""
from __future__ import annotations

import json
import os
import sys

# cases.json lives two directories up from this program's source
# (tools/parity/emitters/py/main.py -> tools/parity/cases.json). Read
# relative to this file, not the process cwd, so the emitter works
# regardless of the harness's invocation directory (parity.py invokes
# with cwd=repo root, but this keeps the emitter runnable standalone too).
_HERE = os.path.dirname(os.path.abspath(__file__))
CASES_PATH = os.path.join(_HERE, "..", "..", "cases.json")

import axon  # noqa: E402
from axon.hooks import Decision, Input, codec_for  # noqa: E402
from axon.hooks.hosts import all as _all  # noqa: E402,F401  (registers the four codecs)


def main() -> None:
    if len(sys.argv) != 2:
        print("usage: main.py <case-id>", file=sys.stderr)
        sys.exit(2)
    case_id = sys.argv[1]

    try:
        with open(CASES_PATH, encoding="utf-8") as f:
            cases = json.load(f)
    except (OSError, json.JSONDecodeError) as exc:
        print(f"py-emitter: load cases.json: {exc}", file=sys.stderr)
        sys.exit(2)

    found = next((c for c in cases if c.get("id") == case_id), None)
    if found is None:
        print(f"py-emitter: unknown case id {case_id!r}", file=sys.stderr)
        sys.exit(2)

    try:
        result = dispatch(found)
    except Exception as exc:  # noqa: BLE001 - surfaced as a process failure
        print(f"py-emitter: {exc}", file=sys.stderr)
        sys.exit(1)

    sys.stdout.write(canonical_json(result) + "\n")


def dispatch(c: dict) -> object:
    call = c.get("call")
    args = c.get("args") or {}
    if call == "resolve":
        return call_resolve(args)
    if call == "hosts":
        return call_hosts()
    if call == "hooked_hosts":
        return call_hooked_hosts()
    if call == "native_events":
        return call_native_events()
    if call == "capability_level":
        return call_capability_level(args)
    if call == "host_event":
        return call_host_event(args)
    if call == "encode_input":
        return call_encode_input(args)
    if call == "decode_decision":
        return call_decode_decision(args)
    return {"unsupported": True}


def _arg_string(args: dict, key: str) -> str:
    v = args.get(key)
    return v if isinstance(v, str) else ""


def _arg_int(args: dict, key: str) -> int:
    v = args.get(key)
    if isinstance(v, bool):
        return 0
    if isinstance(v, (int, float)):
        return int(v)
    return 0


def call_resolve(args: dict) -> dict:
    h = axon.resolve(_arg_string(args, "name"))
    return {"found": h is not None, "name": h.name if h is not None else ""}


def call_hosts() -> list[str]:
    return sorted(h.name for h in axon.hosts())


def call_hooked_hosts() -> list[str]:
    return sorted(h.name for h in axon.hooked_hosts())


def call_native_events() -> list[str]:
    return sorted(axon.native_events())


def _caps_for(host: str):
    return axon.load_capabilities(host)


def call_capability_level(args: dict) -> dict:
    try:
        caps = _caps_for(_arg_string(args, "host"))
        lvl, ok = axon.level(caps, _arg_string(args, "event"))
        return {"level": lvl if lvl is not None else "", "classified": ok}
    except Exception as exc:  # noqa: BLE001
        return _sentinel_result(exc)


def call_host_event(args: dict) -> dict:
    try:
        caps = _caps_for(_arg_string(args, "host"))
        host_event = axon.host_event(caps, _arg_string(args, "event"))
        return {"host_event": host_event if host_event is not None else "", "found": host_event is not None}
    except Exception as exc:  # noqa: BLE001
        return _sentinel_result(exc)


def _read_fixture(host: str, fixture: str) -> str:
    path = os.path.join(axon.spec_root(), "fixtures", "hosts", host, fixture)
    with open(path, encoding="utf-8") as f:
        return f.read()


def _event_from_fixture_name(fixture: str) -> str:
    """Splits "<Event>.<kind>.json" and returns <Event>, matching
    hooks/conformance_test.go's parsing and the Go/TS emitters.
    """
    i = fixture.find(".")
    return fixture if i == -1 else fixture[:i]


def call_encode_input(args: dict) -> object:
    host = _arg_string(args, "host")
    fixture = _arg_string(args, "fixture")

    codec = codec_for(host)
    if codec is None:
        return _sentinel_result(axon.UnknownHostError(host))

    inp: Input | None = None
    try:
        raw = _read_fixture(host, fixture)
    except FileNotFoundError:
        inp = Input(event=_event_from_fixture_name(fixture), session_id="s", cwd="/tmp")
    except OSError as exc:
        return _sentinel_result(axon.SchemaError(str(exc)))

    if inp is None:
        try:
            inp = codec.decode_input(json.loads(raw))
        except Exception as exc:  # noqa: BLE001
            return _sentinel_result(exc)

    try:
        return codec.encode_input(inp)
    except Exception as exc:  # noqa: BLE001
        return _sentinel_result(exc)


def _parse_stdout_literal(raw_str: str) -> object:
    """Decodes a raw stdout literal the same way a codec's decode_decision
    would receive it: empty string stays empty (the "empty stdout"
    fallback case), anything else is parsed as JSON (letting an
    unparsable literal surface as the codec's own SchemaError, same as
    Go/TS).
    """
    if raw_str.strip() == "":
        return ""
    return json.loads(raw_str)


def call_decode_decision(args: dict) -> object:
    host = _arg_string(args, "host")
    fixture = _arg_string(args, "fixture")
    exit_code = _arg_int(args, "exit")

    codec = codec_for(host)
    if codec is None:
        return _sentinel_result(axon.UnknownHostError(host))

    if "raw" in args:
        raw_str = args["raw"] if isinstance(args["raw"], str) else ""
        stdout = _parse_stdout_literal(raw_str)
    else:
        try:
            raw = _read_fixture(host, fixture)
        except OSError as exc:
            return _sentinel_result(axon.SchemaError(str(exc)))
        stdout = json.loads(raw)

    event = _event_from_fixture_name(fixture)
    try:
        decision: Decision = codec.decode_decision(stdout, exit_code)
    except Exception as exc:  # noqa: BLE001
        return _sentinel_result(exc)
    # The fixture's file name carries the event; set it as the reference
    # conformance test does, before reporting the decoded result.
    decision.event = event
    return {
        "action": decision.action,
        "event": decision.event or "",
        "message": decision.message or "",
        "metadata": decision.metadata if decision.metadata is not None else {},
    }


def _sentinel_result(err: Exception) -> dict:
    """Maps a raised exception onto one of the four contract sentinel
    names by instance type. An error not matching any of the four is a
    bug in this emitter, not a valid case outcome, so it is re-raised to
    surface as a process failure rather than silently coerced.
    """
    if isinstance(err, axon.UnknownHostError):
        return {"error": "ErrUnknownHost"}
    if isinstance(err, axon.UnsupportedEventError):
        return {"error": "ErrUnsupportedEvent"}
    if isinstance(err, axon.UnsupportedActionError):
        return {"error": "ErrUnsupportedAction"}
    if isinstance(err, axon.SchemaError):
        return {"error": "ErrSchema"}
    raise RuntimeError(f"unmapped error (not one of the four sentinels): {err!r}") from err


def canonical_json(value: object) -> str:
    """Object keys sorted (recursively), no insignificant whitespace,
    UTF-8, no trailing newline of its own (main() appends exactly one).
    """
    return json.dumps(_sort_keys_deep(value), sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def _sort_keys_deep(value: object) -> object:
    if isinstance(value, list):
        return [_sort_keys_deep(v) for v in value]
    if isinstance(value, dict):
        return {k: _sort_keys_deep(value[k]) for k in sorted(value.keys())}
    return value


if __name__ == "__main__":
    main()
