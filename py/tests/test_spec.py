"""Mirrors hop.top/internal/spectest's ValidateAll / schemaFor, and pins
the OpenCode exit_codes-absent case from contract notes S2a.
"""

from __future__ import annotations

import os

import pytest
import yaml

from axon._spec import list_spec_dir, read_spec_file, spec_root
from axon.errors import SchemaError
from axon.registry import get
from axon.schema import validate


class TestSpecBundlingPrimitives:
    def test_reads_a_file_relative_to_the_resolved_spec_root(self) -> None:
        raw = read_spec_file("version.yaml")
        assert "version:" in raw

    def test_lists_a_directory_relative_to_the_resolved_spec_root(self) -> None:
        entries = list_spec_dir("hosts")
        assert "claude" in entries
        assert "opencode" in entries

    def test_spec_root_resolves_to_an_existing_directory_containing_hosts(self) -> None:
        root = spec_root()
        assert os.path.basename(root) == "spec"
        assert "hosts" in list_spec_dir(".")


def _schema_for(p: str) -> str | None:
    """schemaFor's five path rules, contract notes S4."""
    if p == "version.yaml":
        return "version.schema.json"
    if p == "events.yaml":
        return "events.schema.json"
    parts = p.split("/")
    if len(parts) == 3 and parts[0] == "hosts" and parts[2] in (
        "host.yaml",
        "capabilities.yaml",
        "invoke.yaml",
    ):
        return parts[2].replace(".yaml", ".schema.json")
    return None


def _walk_yaml(dir_: str, out: list[str] | None = None) -> list[str]:
    if out is None:
        out = []
    for entry in list_spec_dir(dir_):
        rel = entry if dir_ == "." else f"{dir_}/{entry}"
        abs_path = os.path.join(spec_root(), rel)
        if os.path.isdir(abs_path):
            _walk_yaml(rel, out)
        elif rel.endswith(".yaml"):
            out.append(rel)
    return out


class TestSchemaForWalkAndValidate:
    """TestEverySpecFileValidates."""

    def test_every_yaml_file_matches_one_of_the_five_schema_for_rules(self) -> None:
        files = _walk_yaml(".")
        assert len(files) > 0
        unmatched = [f for f in files if _schema_for(f) is None]
        assert unmatched == [], f"unmatched .yaml files: {', '.join(unmatched)}"

    def test_every_yaml_file_validates_against_the_schema_its_path_implies(self) -> None:
        files = _walk_yaml(".")
        for f in files:
            schema = _schema_for(f)
            assert schema is not None, f"no schema for {f}"
            doc = yaml.safe_load(read_spec_file(f))
            validate(f, schema, doc)  # raises SchemaError on violation


class TestExitCodesAbsence:
    """Contract notes S2a."""

    def test_opencode_host_has_no_exit_codes_key_at_all(self) -> None:
        h = get("opencode")
        assert h is not None
        assert h.exit_codes is None

    def test_claude_host_declares_exit_codes_with_allow_and_block_present(self) -> None:
        h = get("claude")
        assert h is not None
        assert h.exit_codes is not None
        assert h.exit_codes.allow == 0
        assert h.exit_codes.block == 2


class TestValidateSchemaErrors:
    def test_raises_schema_error_for_a_document_that_violates_its_schema(self) -> None:
        with pytest.raises(SchemaError):
            validate("test", "host.schema.json", {"status": "active"})
