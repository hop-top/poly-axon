#!/usr/bin/env python3
"""Generate tools/parity/cases.json from spec/ and hand-authored cases.

Deterministic: every case list is built, then the whole array is sorted by
id before writing, so re-running this script on an unchanged spec/ tree
produces no diff. Run it after any spec/fixtures/ or spec/hosts/ change,
then commit the regenerated cases.json alongside this script.
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SPEC = ROOT / "spec"
OUT = Path(__file__).resolve().parent / "cases.json"

# Exit code per action, per hooked host. Mirrors hooks/conformance_test.go's
# exitFor: read straight off each host's host.yaml exit_codes (0 when a
# host declares no code for that action, e.g. gemini/opencode warn).
HOST_EXIT = {
    "claude": {"allow": 0, "warn": 0, "block": 2, "rewrite": 3},
    "codex": {"allow": 0, "warn": 0, "block": 2, "rewrite": 3},
    "gemini": {"allow": 0, "warn": 0, "block": 2, "rewrite": 0},
    "opencode": {"allow": 0, "warn": 0, "block": 0, "rewrite": 0},
}

HOOKED_HOSTS = sorted(HOST_EXIT)


def fixture_cases() -> list[dict]:
    cases: list[dict] = []
    for host in HOOKED_HOSTS:
        fixture_dir = SPEC / "fixtures" / "hosts" / host
        for path in sorted(fixture_dir.glob("*.json")):
            stem = path.name[: -len(".json")]
            parts = stem.split(".")
            if len(parts) != 2:
                raise ValueError(f"unexpected fixture name shape: {path}")
            event, kind = parts
            if kind == "input":
                cases.append(
                    {
                        "id": f"encode_input/{host}/{event}",
                        "call": "encode_input",
                        "args": {"host": host, "fixture": path.name},
                    }
                )
            else:
                action = kind
                cases.append(
                    {
                        "id": f"decode_decision/{host}/{event}.{action}",
                        "call": "decode_decision",
                        "args": {
                            "host": host,
                            "fixture": path.name,
                            "exit": HOST_EXIT[host][action],
                        },
                    }
                )
    return cases


def hand_cases() -> list[dict]:
    cases: list[dict] = []

    # resolve: all three published aliases, plus one unknown name.
    aliases = {"claude": "claude-code", "codex": "codex-cli", "gemini": "gemini-cli"}
    for canon, alias in aliases.items():
        cases.append(
            {
                "id": f"resolve/alias-{alias}",
                "call": "resolve",
                "args": {"name": alias},
            }
        )
    cases.append(
        {
            "id": "resolve/unknown",
            "call": "resolve",
            "args": {"name": "not-a-real-host"},
        }
    )

    # hosts / hooked_hosts / native_events, once each.
    cases.append({"id": "hosts/all", "call": "hosts", "args": {}})
    cases.append({"id": "hooked_hosts/all", "call": "hooked_hosts", "args": {}})
    cases.append({"id": "native_events/all", "call": "native_events", "args": {}})

    # capability_level: native, close, synthesized, unsupported on >= 2 hosts.
    level_cases = [
        ("codex", "SessionStart"),  # native
        ("codex", "PreToolUse"),  # close
        ("codex", "CwdChanged"),  # synthesized
        ("codex", "TeammateIdle"),  # unsupported
        ("gemini", "SessionStart"),  # native
        ("gemini", "UserPromptSubmit"),  # close
        ("gemini", "PermissionRequest"),  # synthesized
        ("gemini", "StopFailure"),  # unsupported
    ]
    for host, event in level_cases:
        cases.append(
            {
                "id": f"capability_level/{host}/{event}",
                "call": "capability_level",
                "args": {"host": host, "event": event},
            }
        )

    # host_event: a native row, a close-only row (opencode has these), and
    # an unmapped event.
    host_event_cases = [
        ("claude", "PreToolUse"),  # native
        ("opencode", "TaskCompleted"),  # close-only
        ("opencode", "TeammateIdle"),  # unmapped
    ]
    for host, event in host_event_cases:
        cases.append(
            {
                "id": f"host_event/{host}/{event}",
                "call": "host_event",
                "args": {"host": host, "event": event},
            }
        )

    # Error sentinels, first-class. TeammateIdle has no captured fixture
    # for codex (it is unsupported there), so this fixture name is not on
    # disk; per the README, an emitter then builds a minimal synthetic
    # Input for the named event instead of decoding a real file.
    cases.append(
        {
            "id": "encode_input/codex/unsupported-event",
            "call": "encode_input",
            "args": {"host": "codex", "fixture": "TeammateIdle.input.json"},
        }
    )
    cases.append(
        {
            "id": "decode_decision/unknown-host",
            "call": "decode_decision",
            "args": {"host": "not-a-real-host", "fixture": "PreToolUse.block.json", "exit": 2},
        }
    )

    # The no-fixture fallback cases from docs/spec-contract-notes.md §6:
    # each of claude/gemini/codex's DecodeDecision recognizes none of
    # these as a known decision shape and falls back to actionForExit(exit)
    # with no error -- this is the fail-open gate every codec with an
    # exit-code contract must have (opencode is exempt: it has no
    # exit_codes and DecodeDecision ignores its exit argument entirely).
    # "raw" carries the literal stdout bytes; "fixture" still supplies the
    # <Event> prefix used to set Decision.Event on the result.
    for host in ("claude", "gemini", "codex"):
        for name, raw in (
            ("empty-stdout", ""),
            ("empty-object", "{}"),
            ("unrecognized-json", '{"foo":"bar"}'),
        ):
            cases.append(
                {
                    "id": f"decode_decision/{host}/fallback-{name}",
                    "call": "decode_decision",
                    "args": {
                        "host": host,
                        "fixture": "PreToolUse.block.json",
                        "exit": HOST_EXIT[host]["block"],
                        "raw": raw,
                    },
                }
            )

    # SessionStart allow metadata: claude and codex carry
    # additional_context / system_message / continue in Decision.Metadata.
    # These two are also covered by the fixture-driven generator above
    # (decode_decision/claude/SessionStart.allow, decode_decision/codex/
    # SessionStart.allow); listed here only as the documented minimum this
    # generator must include, not duplicated.

    return cases


def main() -> int:
    cases = fixture_cases() + hand_cases()

    ids = [c["id"] for c in cases]
    dupes = sorted({i for i in ids if ids.count(i) > 1})
    if dupes:
        print(f"gencases: duplicate case ids: {dupes}", file=sys.stderr)
        return 1

    cases.sort(key=lambda c: c["id"])

    text = json.dumps(cases, indent=2, sort_keys=True) + "\n"
    OUT.write_text(text)
    print(f"gencases: wrote {len(cases)} cases to {OUT}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
