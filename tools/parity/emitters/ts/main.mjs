#!/usr/bin/env node
// The axon parity emitter for TypeScript. Answers one parity-harness case
// against the built ts/dist/ package. See tools/parity/README.md for the
// invocation contract this program implements; see
// go/tools/parity/main.go for the reference this mirrors
// operation-for-operation.
//
// Imports the BUILT package (not ts/src/ directly) so this emitter
// exercises exactly what a consumer of @hop-top/axon would run --
// `make build-ts` must run before this emitter is invoked (wired in the
// Makefile's test-parity target).
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import * as axon from "../../../../ts/dist/index.js";
import { codecFor } from "../../../../ts/dist/hooks/index.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
// cases.json lives two directories up from this program's source
// (tools/parity/emitters/ts/main.mjs -> tools/parity/cases.json). Read
// relative to this file, not process.cwd(), so the emitter works
// regardless of the harness's invocation directory (parity.py invokes
// with cwd=repo root, but this keeps the emitter runnable standalone too).
const CASES_PATH = path.join(__dirname, "..", "..", "cases.json");

function main() {
  const args = process.argv.slice(2);
  if (args.length !== 1) {
    process.stderr.write("usage: main.mjs <case-id>\n");
    process.exit(2);
  }
  const caseId = args[0];

  let cases;
  try {
    cases = JSON.parse(readFileSync(CASES_PATH, "utf8"));
  } catch (err) {
    process.stderr.write(`ts-emitter: load cases.json: ${err}\n`);
    process.exit(2);
  }
  const found = cases.find((c) => c.id === caseId);
  if (!found) {
    process.stderr.write(`ts-emitter: unknown case id ${JSON.stringify(caseId)}\n`);
    process.exit(2);
  }

  let result;
  try {
    result = dispatch(found);
  } catch (err) {
    process.stderr.write(`ts-emitter: ${err && err.stack ? err.stack : err}\n`);
    process.exit(1);
  }
  process.stdout.write(canonicalJSON(result) + "\n");
}

function dispatch(c) {
  switch (c.call) {
    case "resolve":
      return callResolve(c.args);
    case "hosts":
      return callHosts();
    case "hooked_hosts":
      return callHookedHosts();
    case "native_events":
      return callNativeEvents();
    case "capability_level":
      return callCapabilityLevel(c.args);
    case "host_event":
      return callHostEvent(c.args);
    case "encode_input":
      return callEncodeInput(c.args);
    case "decode_decision":
      return callDecodeDecision(c.args);
    default:
      return { unsupported: true };
  }
}

function argString(args, key) {
  const v = args[key];
  return typeof v === "string" ? v : "";
}

function argInt(args, key) {
  const v = args[key];
  return typeof v === "number" ? Math.trunc(v) : 0;
}

function callResolve(args) {
  const h = axon.resolve(argString(args, "name"));
  return { found: h !== undefined, name: h ? h.name : "" };
}

function callHosts() {
  return axon.hosts().map((h) => h.name).sort();
}

function callHookedHosts() {
  return axon.hookedHosts().map((h) => h.name).sort();
}

function callNativeEvents() {
  return [...axon.nativeEvents()].sort();
}

function capsFor(host) {
  return axon.loadCapabilities(host);
}

function callCapabilityLevel(args) {
  try {
    const caps = capsFor(argString(args, "host"));
    const { level, ok } = axon.level(caps, argString(args, "event"));
    return { level, classified: ok };
  } catch (err) {
    return sentinelResult(err);
  }
}

function callHostEvent(args) {
  try {
    const caps = capsFor(argString(args, "host"));
    const hostEvent = axon.hostEvent(caps, argString(args, "event"));
    return { host_event: hostEvent ?? "", found: hostEvent !== undefined };
  } catch (err) {
    return sentinelResult(err);
  }
}

function readFixture(host, fixture) {
  return readFileSync(path.join(axon.specRoot(), "fixtures", "hosts", host, fixture), "utf8");
}

// eventFromFixtureName splits "<Event>.<kind>.json" and returns <Event>,
// matching hooks/conformance_test.go's parsing and the Go emitter.
function eventFromFixtureName(fixture) {
  const i = fixture.indexOf(".");
  return i === -1 ? fixture : fixture.slice(0, i);
}

// callEncodeInput answers `encode_input`. When the named fixture exists
// on disk, it decodes the real file and re-encodes it. A fixture name
// that does not exist on disk probes the UnsupportedEventError path: the
// <Event> is parsed from the fixture name and a minimal synthetic Input
// is built directly, mirroring the Go emitter exactly.
function callEncodeInput(args) {
  const host = argString(args, "host");
  const fixture = argString(args, "fixture");

  const codec = codecFor(host);
  if (!codec) {
    return sentinelResult(new axon.UnknownHostError(host));
  }

  let input;
  let raw;
  try {
    raw = readFixture(host, fixture);
  } catch (err) {
    if (err && err.code === "ENOENT") {
      input = { event: eventFromFixtureName(fixture), sessionId: "s", cwd: "/tmp" };
    } else {
      return sentinelResult(new axon.SchemaError(String(err)));
    }
  }
  if (raw !== undefined) {
    try {
      input = codec.decodeInput(JSON.parse(raw));
    } catch (err) {
      return sentinelResult(err);
    }
  }

  try {
    return codec.encodeInput(input);
  } catch (err) {
    return sentinelResult(err);
  }
}

// callDecodeDecision answers `decode_decision`. When args carries a "raw"
// string, that literal string is decoded instead of a fixture file --
// this is how the three fallback cases (empty stdout, "{}", unrecognized
// JSON, all with a block exit) are expressed without inventing fixture
// files nobody captured from a host.
function callDecodeDecision(args) {
  const host = argString(args, "host");
  const fixture = argString(args, "fixture");
  const exit = argInt(args, "exit");

  const codec = codecFor(host);
  if (!codec) {
    return sentinelResult(new axon.UnknownHostError(host));
  }

  let stdout;
  if (Object.prototype.hasOwnProperty.call(args, "raw")) {
    const rawStr = typeof args.raw === "string" ? args.raw : "";
    stdout = parseStdoutLiteral(rawStr);
  } else {
    let raw;
    try {
      raw = readFixture(host, fixture);
    } catch (err) {
      return sentinelResult(new axon.SchemaError(String(err)));
    }
    stdout = JSON.parse(raw);
  }

  const event = eventFromFixtureName(fixture);
  let decision;
  try {
    decision = codec.decodeDecision(stdout, exit);
  } catch (err) {
    return sentinelResult(err);
  }
  // The fixture's file name carries the event; set it as the reference
  // conformance test does, before reporting the decoded result.
  decision.event = event;
  return {
    action: decision.action,
    event: decision.event ?? "",
    message: decision.message ?? "",
    metadata: decision.metadata ?? {},
  };
}

// parseStdoutLiteral decodes a raw stdout literal the same way a codec's
// decodeDecision would receive it: empty string stays empty (the "empty
// stdout" fallback case), anything else is parsed as JSON (letting an
// unparsable literal surface as the codec's own ErrSchema, same as Go).
function parseStdoutLiteral(rawStr) {
  if (rawStr.trim() === "") return "";
  return JSON.parse(rawStr);
}

// sentinelResult maps a thrown error onto one of the four contract
// sentinel names by instance type. An error not matching any of the four
// is a bug in this emitter, not a valid case outcome, so it is rethrown
// to surface as a process failure rather than silently coerced.
function sentinelResult(err) {
  if (err instanceof axon.UnknownHostError) return { error: "ErrUnknownHost" };
  if (err instanceof axon.UnsupportedEventError) return { error: "ErrUnsupportedEvent" };
  if (err instanceof axon.UnsupportedActionError) return { error: "ErrUnsupportedAction" };
  if (err instanceof axon.SchemaError) return { error: "ErrSchema" };
  throw new Error(`unmapped error (not one of the four sentinels): ${err && err.stack ? err.stack : err}`);
}

// canonicalJSON: object keys sorted (recursively), no insignificant
// whitespace, UTF-8 (native to JS strings/Buffers), no trailing newline
// of its own (main() appends exactly one).
function canonicalJSON(value) {
  return JSON.stringify(sortKeysDeep(value));
}

function sortKeysDeep(value) {
  if (Array.isArray(value)) return value.map(sortKeysDeep);
  if (value !== null && typeof value === "object") {
    const out = {};
    for (const key of Object.keys(value).sort()) {
      out[key] = sortKeysDeep(value[key]);
    }
    return out;
  }
  return value;
}

main();
