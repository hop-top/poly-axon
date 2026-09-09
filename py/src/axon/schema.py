"""Draft-07 JSON Schema validation for spec/ documents.

All 47 schema files in spec/ declare
``"$schema": "http://json-schema.org/draft-07/schema#"`` (contract notes
S5). ``jsonschema.Draft7Validator`` implements draft-07 semantics, confirmed
against this exact schema shape: ``if``/``then`` (host.schema.json's one
conditional), ``$ref`` + ``definitions`` (events/capabilities/invoke
schemas), ``enum``, ``pattern``, ``uniqueItems``, ``minItems``, and
``additionalProperties`` all validate as draft-07 specifies with no extra
registration needed.
"""

from __future__ import annotations

import json
from typing import Any

from jsonschema import Draft7Validator

from axon._spec import read_spec_file
from axon.errors import SchemaError

_compiled_cache: dict[str, Draft7Validator] = {}


def _compile(schema_path: str) -> Draft7Validator:
    cached = _compiled_cache.get(schema_path)
    if cached is not None:
        return cached

    try:
        schema = json.loads(read_spec_file(schema_path))
    except (OSError, json.JSONDecodeError) as err:
        raise SchemaError(f"read/parse schema {schema_path}: {err}") from err

    try:
        Draft7Validator.check_schema(schema)
        validator = Draft7Validator(schema)
    except Exception as err:  # noqa: BLE001 - surfaced as our own sentinel
        raise SchemaError(f"compile schema {schema_path}: {err}") from err

    _compiled_cache[schema_path] = validator
    return validator


def validate(path: str, schema_path: str, doc: Any) -> None:
    """Validates doc (already parsed from YAML or JSON) against schema_path
    (relative to the spec root) as draft-07. Raises SchemaError on any
    violation; path is carried in the error message for diagnostics only.
    """
    validator = _compile(schema_path)
    errors = sorted(validator.iter_errors(doc), key=lambda e: list(e.path))
    if errors:
        details = "; ".join(f"{'/'.join(str(p) for p in e.path) or '/'} {e.message}" for e in errors)
        raise SchemaError(f"{path} violates {schema_path}: {details}")
