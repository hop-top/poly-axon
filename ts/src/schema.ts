import Ajv, { type Schema, type ValidateFunction } from "ajv";
import { readSpecFile } from "./spec";
import { SchemaError } from "./errors";

// All 47 schema files in spec/ declare
// "$schema": "http://json-schema.org/draft-07/schema#" (contract notes
// §5). ajv v8's default `Ajv` class (as opposed to its `Ajv2019`/`Ajv2020`
// subclasses) targets draft-07 semantics, confirmed empirically against
// this exact schema shape: `if`/`then` (host.schema.json's one
// conditional), `$ref` + `definitions` (events/capabilities/invoke
// schemas), `enum`, `pattern`, `uniqueItems`, `minItems`, and
// `additionalProperties` all validate as draft-07 specifies with no
// extra metaschema registration needed.
const ajv = new Ajv({ strict: false });

const compiledCache = new Map<string, ValidateFunction>();

function compile(schemaPath: string): ValidateFunction {
  const cached = compiledCache.get(schemaPath);
  if (cached) return cached;

  let schema: Schema;
  try {
    schema = JSON.parse(readSpecFile(schemaPath));
  } catch (err) {
    throw new SchemaError(`read/parse schema ${schemaPath}: ${(err as Error).message}`);
  }

  let compiled: ValidateFunction;
  try {
    compiled = ajv.compile(schema);
  } catch (err) {
    throw new SchemaError(`compile schema ${schemaPath}: ${(err as Error).message}`);
  }
  compiledCache.set(schemaPath, compiled);
  return compiled;
}

/**
 * Validates doc (already parsed from YAML or JSON) against schemaPath
 * (relative to the spec root) as draft-07. Throws SchemaError on any
 * violation; path is carried in the error message for diagnostics only.
 */
export function validate(path: string, schemaPath: string, doc: unknown): void {
  const validateFn = compile(schemaPath);
  if (!validateFn(doc)) {
    const details = validateFn.errors?.map((e) => `${e.instancePath || "/"} ${e.message}`).join("; ");
    throw new SchemaError(`${path} violates ${schemaPath}: ${details}`);
  }
}
