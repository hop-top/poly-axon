import { describe, expect, it } from "vitest";
import path from "node:path";
import { statSync } from "node:fs";
import YAML from "yaml";
import { readSpecFile, listSpecDir, specRoot } from "../src/spec";
import { validate, SchemaError } from "../src/schema";
import { get } from "../src/index";

// Mirrors hop.top/internal/spectest's ValidateAll / schemaFor, and pins
// the OpenCode exit_codes-absent case from contract notes §2a.

describe("spec bundling primitives", () => {
  it("reads a file relative to the resolved spec root", () => {
    const raw = readSpecFile("version.yaml");
    expect(raw).toContain("version:");
  });

  it("lists a directory relative to the resolved spec root", () => {
    const entries = listSpecDir("hosts");
    expect(entries).toContain("claude");
    expect(entries).toContain("opencode");
  });

  it("specRoot() resolves to an existing directory containing hosts/", () => {
    const root = specRoot();
    expect(path.basename(root)).toBe("spec");
    expect(listSpecDir(".")).toContain("hosts");
  });
});

describe("schemaFor / walk-and-validate (TestEverySpecFileValidates)", () => {
  // schemaFor's five path rules, contract notes §4.
  function schemaFor(p: string): string | undefined {
    if (p === "version.yaml") return "version.schema.json";
    if (p === "events.yaml") return "events.schema.json";
    const hostsMatch = p.match(/^hosts\/[^/]+\/(host|capabilities|invoke)\.yaml$/);
    if (hostsMatch) return `${hostsMatch[1]}.schema.json`;
    return undefined;
  }

  function walkYaml(dir: string, out: string[] = []): string[] {
    for (const entry of listSpecDir(dir)) {
      const rel = dir === "." ? entry : `${dir}/${entry}`;
      const abs = path.join(specRoot(), rel);
      const isDir = statSync(abs).isDirectory();
      if (isDir) {
        walkYaml(rel, out);
      } else if (rel.endsWith(".yaml")) {
        out.push(rel);
      }
    }
    return out;
  }

  it("every .yaml file under spec/ matches one of the five schemaFor rules", () => {
    const files = walkYaml(".");
    expect(files.length).toBeGreaterThan(0);
    const unmatched = files.filter((f) => schemaFor(f) === undefined);
    expect(unmatched, `unmatched .yaml files: ${unmatched.join(", ")}`).toEqual([]);
  });

  it("every .yaml file under spec/ validates against the schema its path implies", () => {
    const files = walkYaml(".");
    for (const f of files) {
      const schema = schemaFor(f);
      expect(schema, `no schema for ${f}`).toBeDefined();
      const doc = YAML.parse(readSpecFile(f));
      expect(() => validate(f, schema as string, doc), `${f} vs ${schema}`).not.toThrow();
    }
  });
});

describe("exitCodes absence (contract notes §2a)", () => {
  it("OpenCode's host has no exitCodes key at all", () => {
    const h = get("opencode");
    expect(h).toBeDefined();
    expect(h && "exitCodes" in h).toBe(false);
    expect(h?.exitCodes).toBeUndefined();
  });

  it("claude's host declares exitCodes with allow/block present", () => {
    const h = get("claude");
    expect(h?.exitCodes).toBeDefined();
    expect(h?.exitCodes?.allow).toBe(0);
    expect(h?.exitCodes?.block).toBe(2);
  });
});

describe("validate() schema errors", () => {
  it("throws SchemaError for a document that violates its schema", () => {
    expect(() => validate("test", "host.schema.json", { status: "active" })).toThrow(SchemaError);
  });
});
