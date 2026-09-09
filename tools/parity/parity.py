#!/usr/bin/env python3
"""Cross-language conformance harness for hop.top/axon bindings.

Runs every registered emitter against every case in cases.json and
compares parsed JSON values (not raw bytes, so key order cannot cause a
false failure). See tools/parity/README.md for the full contract.

Go is the reference implementation: every other language is compared
against it, never against a majority of bindings. REFERENCE names it, and
the harness refuses to run a case whose reference did not answer rather
than silently promoting a binding into that role.

Exit codes: 0 all cases agree; 1 a divergence; 2 a usage/setup error.
"""
from __future__ import annotations

import contextlib
import dataclasses
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any, Iterator

ROOT = Path(__file__).resolve().parents[2]
PARITY_DIR = Path(__file__).resolve().parent
CASES_PATH = PARITY_DIR / "cases.json"

UNSUPPORTED = {"unsupported": True}


@dataclasses.dataclass(frozen=True)
class Language:
    """One row of the language table. Adding a language is one more row.

    build: argv run once, before any case; "{bin}" in its argv is
        replaced with a fresh path the build must produce an executable
        at. None means the language needs no build step (e.g. an
        interpreted emitter run directly).
    run: argv template for one case; "{bin}" is replaced with the path
        built above (or, with no build step, is the literal command to
        run — e.g. ["node", "tools/parity/emitters/ts/main.js"]).
    build_dir: directory the build argv runs in, relative to ROOT. Needed
        by a language whose build tool must run inside its own directory
        — Go's emitter lives inside the module at go/tools/parity, so its
        `go build` needs cwd=ROOT/go to find go.mod. None means ROOT.
        Only the build moves: every emitter is *run* from ROOT, because
        the frozen contract has emitters read tools/parity/cases.json by
        that relative path.
    unset_env: environment variables removed from the build's environment.
        Empty for a language that needs no such guard.
    """

    name: str
    build: list[str] | None
    run: list[str]
    build_dir: str | None = None
    unset_env: tuple[str, ...] = ()

    def build_cwd(self) -> Path:
        """Absolute directory this language's build argv runs in."""
        return ROOT if self.build_dir is None else ROOT / self.build_dir

    def build_env(self) -> dict[str, str] | None:
        """Environment for the build, or None to inherit unchanged."""
        if not self.unset_env:
            return None
        env = os.environ.copy()
        for name in self.unset_env:
            env.pop(name, None)
        return env


LANGUAGES: list[Language] = [
    Language(
        name="go",
        # The emitter imports hop.top/axon, so it lives inside the module
        # at go/tools/parity and its build needs cwd=ROOT/go to find
        # go.mod. "{bin}" is an absolute path, so build_dir does not
        # affect where the binary lands. GOROOT is unset for the build:
        # a stale GOROOT inherited from the environment makes a
        # mise-managed toolchain fail to resolve its own standard library.
        build=["go", "build", "-buildvcs=false", "-o", "{bin}", "./tools/parity"],
        build_dir="go",
        unset_env=("GOROOT",),
        run=["{bin}"],
    ),
    Language(
        name="ts",
        build=None,
        run=["node", "tools/parity/emitters/ts/main.mjs"],
    ),
    Language(
        name="py",
        build=None,
        run=["py/.venv/bin/python3", "tools/parity/emitters/py/main.py"],
    ),
]

# The authoritative language. Every other language's output is compared
# against this one's, so a divergence always reads as a binding
# disagreeing with the reference rather than as a vote to adjudicate.
# Order in LANGUAGES is irrelevant: the reference is named here, and
# require_reference() enforces that it actually answered.
REFERENCE = "go"


class SetupError(Exception):
    """A usage or setup problem: exit code 2, never 1."""


def main() -> int:
    try:
        cases = load_cases()
        with build_emitters(LANGUAGES) as emitters:
            require_reference(emitters)
            return run(cases, emitters)
    except SetupError as exc:
        print(f"parity: {exc}", file=sys.stderr)
        return 2


def run(cases: list[dict], emitters: dict[str, list[str]]) -> int:
    diverged = False
    skipped = 0
    answered_cases = 0

    for case in cases:
        outputs = {lang: run_emitter(lang, cmd, case["id"]) for lang, cmd in emitters.items()}
        supported = {lang: out for lang, out in outputs.items() if out != UNSUPPORTED}
        if not supported:
            skipped += 1
            continue
        answered_cases += 1

        if REFERENCE not in supported:
            raise SetupError(
                f"{case['id']}: reference {REFERENCE} returned unsupported while "
                f"{', '.join(sorted(supported))} answered; a binding must never "
                f"stand in as the reference"
            )
        reference = supported[REFERENCE]
        mismatched = [lang for lang, out in supported.items() if out != reference]
        if mismatched:
            diverged = True
            print(f"parity: DIVERGE {case['id']}: {REFERENCE} vs {', '.join(mismatched)}", file=sys.stderr)
            for lang in mismatched:
                print_diff(reference, supported[lang], [case["id"]])

    if answered_cases == 0:
        print("parity: no case was answered by any emitter", file=sys.stderr)
        return 2
    if diverged:
        return 1

    print(f"parity: ok {' '.join(emitters)} ({answered_cases} cases, {skipped} skipped)")
    return 0


def require_reference(emitters: dict[str, list[str]]) -> None:
    """Fails the run unless the reference language is present and runnable.

    Catches a REFERENCE naming a language absent from LANGUAGES, and
    (because build_emitters raises on a failed build before this point)
    documents that a reference that cannot build is a setup error rather
    than a run whose reference quietly becomes a binding.
    """
    if REFERENCE not in emitters:
        raise SetupError(
            f"reference language {REFERENCE!r} is not in LANGUAGES "
            f"({', '.join(emitters) or 'empty'})"
        )


def load_cases() -> list[dict]:
    try:
        raw = CASES_PATH.read_text()
    except OSError as exc:
        raise SetupError(f"cannot read {CASES_PATH}: {exc}") from exc
    try:
        cases = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise SetupError(f"{CASES_PATH} is not valid JSON: {exc}") from exc
    if not isinstance(cases, list):
        raise SetupError(f"{CASES_PATH} must be a JSON array")

    ids = [c.get("id") for c in cases]
    dupes = sorted({i for i in ids if ids.count(i) > 1})
    if dupes:
        raise SetupError(f"{CASES_PATH} has duplicate case ids: {dupes}")
    for c in cases:
        for key in ("id", "call", "args"):
            if key not in c:
                raise SetupError(f"case missing {key!r}: {c}")
    return cases


@contextlib.contextmanager
def build_emitters(languages: list[Language]) -> Iterator[dict[str, list[str]]]:
    """Builds every language in the table once and yields {name: run argv}.

    Building each emitter once up front (rather than re-invoking a build
    tool per case) keeps a 65-case, one-language run to a couple of
    seconds instead of ~15s, and that gap only widens as more cases and
    languages are added.
    """
    with tempfile.TemporaryDirectory(prefix="axon-parity-") as tmp:
        emitters: dict[str, list[str]] = {}
        for lang in languages:
            bin_path = str(Path(tmp) / lang.name)
            if lang.build is not None:
                tool = lang.build[0]
                if shutil.which(tool) is None:
                    raise SetupError(f"{lang.name} emitter: `{tool}` not found on PATH")
                argv = [bin_path if arg == "{bin}" else arg for arg in lang.build]
                proc = subprocess.run(
                    argv,
                    cwd=str(lang.build_cwd()),
                    env=lang.build_env(),
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    check=False,
                )
                if proc.returncode != 0:
                    raise SetupError(f"{lang.name} emitter: build failed:\n{proc.stdout}{proc.stderr}")
            emitters[lang.name] = [bin_path if arg == "{bin}" else arg for arg in lang.run]
        yield emitters


def run_emitter(lang: str, command: list[str], case_id: str) -> Any:
    proc = subprocess.run(
        [*command, case_id],
        cwd=str(ROOT),
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if proc.returncode != 0:
        print(f"parity: {lang} emitter failed on {case_id} (exit {proc.returncode})", file=sys.stderr)
        if proc.stderr:
            print(proc.stderr, file=sys.stderr)
        sys.exit(2)
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        print(f"parity: {lang} emitted invalid JSON for {case_id}: {exc}", file=sys.stderr)
        print(proc.stdout, file=sys.stderr)
        sys.exit(2)


def print_diff(left: Any, right: Any, path: list[str]) -> None:
    if type(left) is not type(right):
        print(f"  {'.'.join(path)}: type {type(left).__name__} != {type(right).__name__}", file=sys.stderr)
        return
    if isinstance(left, dict):
        for key in sorted(set(left) | set(right)):
            if key not in left:
                print(f"  {'.'.join(path + [str(key)])}: missing from reference", file=sys.stderr)
                return
            if key not in right:
                print(f"  {'.'.join(path + [str(key)])}: missing from candidate", file=sys.stderr)
                return
            if left[key] != right[key]:
                print_diff(left[key], right[key], path + [str(key)])
                return
        return
    if isinstance(left, list):
        if len(left) != len(right):
            print(f"  {'.'.join(path)}: len {len(left)} != {len(right)}", file=sys.stderr)
            return
        for i, (a, b) in enumerate(zip(left, right)):
            if a != b:
                print_diff(a, b, path + [str(i)])
                return
        return
    print(f"  {'.'.join(path)}: {left!r} != {right!r}", file=sys.stderr)


if __name__ == "__main__":
    raise SystemExit(main())
