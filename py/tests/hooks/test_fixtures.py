"""Mirrors hop.top/axon/hooks/conformance_test.go's
TestFixturesRoundTripAndValidate over the four registered codecs (claude,
codex, gemini, opencode -- contract notes S4). Per contract notes S6:

- input fixtures: decode-then-encode must be JSON-value-equal to the file,
  then the re-encoded output must validate against the <Event>.input
  schema.
- decision fixtures: decode must report the action the file name names,
  decode sets decision.event from the file name, re-encode must validate
  against the <Event>.<action> schema -- value-equality against the
  original file is explicitly NOT required (a decision fixture is not
  guaranteed to be what the codec itself would emit; see
  docs/spec-contract-notes.md S6 and S8 for the stricter byte-equal rule
  this task's per-codec tests pin separately for the specific cases S8
  names, e.g. claude/codex SessionStart.allow).
"""

from __future__ import annotations

import json
import os

import pytest

from axon import get, validate
from axon.hooks import Action, codec_for
from axon.hooks.hosts import all as _all  # noqa: F401  (registers the four codecs)

HOOKED_HOSTS = ["claude", "codex", "gemini", "opencode"]

ACTIONS: list[Action] = ["allow", "warn", "block", "rewrite"]


def _fixture_dir(host: str) -> str:
    from axon import spec_root

    return os.path.join(spec_root(), "fixtures", "hosts", host)


def _read_json(path: str) -> object:
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def _exit_for(host: str, action: Action) -> int:
    """The host's declared exit code for the action, read from host.yaml
    via the reader (never a hard-coded per-host literal table). Hosts with
    no exit-code contract (opencode) pass 0 -- decode_decision for
    opencode ignores its exit argument entirely.
    """
    h = get(host)
    codes = h.exit_codes if h else None
    if codes is None:
        return 0
    if action == "allow":
        return codes.allow
    if action == "warn":
        return codes.warn if codes.warn is not None else codes.allow
    if action == "block":
        return codes.block
    if action == "rewrite":
        return codes.rewrite if codes.rewrite is not None else codes.allow
    raise ValueError(action)


def _fixture_entries() -> list[tuple[str, str]]:
    entries: list[tuple[str, str]] = []
    for host in HOOKED_HOSTS:
        for name in sorted(os.listdir(_fixture_dir(host))):
            if name.endswith(".json"):
                entries.append((host, name))
    return entries


_ENTRIES = _fixture_entries()


class TestFixturesRoundTripAndValidate:
    @pytest.mark.parametrize("host", HOOKED_HOSTS)
    def test_has_at_least_one_fixture(self, host: str) -> None:
        entries = [n for n in os.listdir(_fixture_dir(host)) if n.endswith(".json")]
        assert len(entries) > 0, f"no fixtures for {host}"

    def test_fixture_count_matches_expectation(self) -> None:
        """Asserts the total fixture count so a silently skipped host
        (e.g. a typo in HOOKED_HOSTS, or a directory glob that matched
        nothing) fails loudly instead of quietly running zero cases.
        """
        assert len(_ENTRIES) == 42

    @pytest.mark.parametrize("host,entry", _ENTRIES, ids=[f"{h}/{e}" for h, e in _ENTRIES])
    def test_fixture(self, host: str, entry: str) -> None:
        codec = codec_for(host)
        assert codec is not None, f"no codec registered for {host}"

        stem = entry[: -len(".json")]
        parts = stem.split(".")
        assert len(parts) == 2, f"unexpected fixture name shape: {entry}"
        event, kind = parts

        raw = _read_json(os.path.join(_fixture_dir(host), entry))

        if kind == "input":
            inp = codec.decode_input(raw)
            again = codec.encode_input(inp)
            assert again == raw
            validate(f"{host}/{entry}", f"hosts/{host}/hooks/{event}.input.schema.json", again)
        else:
            action = kind
            assert action in ACTIONS, f"unexpected action in fixture name: {entry}"
            decision = codec.decode_decision(raw, _exit_for(host, action))
            assert decision.action == action
            # The fixture's file name carries the event; set it before
            # re-encoding, same as the Go conformance test, so codecs
            # whose wire shape varies by event re-encode the right shape.
            decision.event = event
            stdout, _exit = codec.encode_decision(decision)
            validate(f"{host}/{entry}", f"hosts/{host}/hooks/{event}.{action}.schema.json", stdout)
