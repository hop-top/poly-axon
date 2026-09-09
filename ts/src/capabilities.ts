import YAML from "yaml";
import { readSpecFile } from "./spec";
import { validate } from "./schema";
import { get } from "./registry";
import type { Event } from "./events";
import { UnknownHostError, SchemaError } from "./errors";

/** How a host supports a canonical event. */
export type Level = "native" | "close" | "synthesized" | "unsupported";

/** A host-side event name mapped to a canonical event. */
export interface Pair {
  hostEvent: string;
  event: Event;
  note?: string;
}

/**
 * How a host synthesizes an event it lacks natively. Every field is
 * optional (contract notes §2b): `technique` and `cost` absent means the
 * spec does not classify that row -- never default them to a sentinel
 * that participates in matching logic.
 */
export interface Recipe {
  technique?: string;
  source?: string;
  via?: string;
  pattern?: string;
  cost?: string;
  notes?: string;
}

/** A host's parsed capabilities.yaml. */
export interface Capabilities {
  host: string;
  hostVersionRange?: string;
  native: Pair[];
  close?: Pair[];
  // Partial, not Record<Event, Recipe>: a host's capabilities.yaml
  // synthesizes only a subset of events, and TypeScript's Record<K, V>
  // over a closed union K requires every key present.
  synthesized?: Partial<Record<Event, Recipe>>;
  unsupported?: Event[];
}

interface RawPair {
  host_event: string;
  event: string;
  note?: string;
}

interface RawRecipe {
  technique?: string;
  source?: string;
  via?: string;
  pattern?: string;
  cost?: string;
  notes?: string;
}

interface RawCapabilities {
  host: string;
  host_version_range?: string;
  native: RawPair[];
  close?: RawPair[];
  synthesized?: Record<string, RawRecipe>;
  unsupported?: string[];
}

// p.event is read from a hooked host's capabilities.yaml at runtime; the
// JSON schema constrains it to `string`, not to the closed Event union
// (an entry naming an event not in the current catalog is exactly what
// generate-check's staleness detection exists to catch -- see
// ts/scripts/gen-events.mjs -- not something this loader enforces
// statically). The cast documents that trust boundary.
function toPair(p: RawPair): Pair {
  const pair: Pair = { hostEvent: p.host_event, event: p.event as Event };
  if (p.note !== undefined) pair.note = p.note;
  return pair;
}

/**
 * Loads and validates the bundled capabilities.yaml for a hooked host.
 *
 * Throws UnknownHostError ONLY when `host` is not a name axon recognizes
 * at all (canonical names only, matching hooks.LoadCapabilities's
 * ErrUnknownHost branch in Go). A recognized host with no
 * capabilities.yaml -- every identity-only host, e.g. amp -- throws
 * SchemaError instead: the name is fine, but there is nothing to load.
 * These are never conflated: a caller must be able to branch on "this
 * name is garbage" (UnknownHostError) versus "this host has no hook
 * contract" (SchemaError). Never returns an empty Capabilities value on
 * failure.
 */
export function loadCapabilities(host: string): Capabilities {
  if (get(host) === undefined) {
    throw new UnknownHostError(host);
  }

  const relPath = `hosts/${host}/capabilities.yaml`;
  let raw: string;
  try {
    raw = readSpecFile(relPath);
  } catch (err) {
    // host is recognized (checked above); a missing/unreadable
    // capabilities.yaml is a load-shaped failure, not an unknown-host
    // one -- every identity-only host (e.g. amp) hits this path and is
    // NOT "unknown". Matches hooks.LoadCapabilities in Go: ErrUnknownHost
    // is returned only from axon.Get's not-found branch; a failure from
    // LoadSpecYAML (missing file, parse error, schema violation) always
    // propagates as its own load error, never re-labeled as unknown-host.
    throw new SchemaError(`${host}: no capabilities.yaml at ${relPath} (${(err as Error).message})`);
  }

  let doc: RawCapabilities;
  try {
    doc = YAML.parse(raw) as RawCapabilities;
  } catch (err) {
    throw new SchemaError(`parse ${relPath}: ${(err as Error).message}`);
  }
  validate(relPath, "capabilities.schema.json", doc);

  const caps: Capabilities = {
    host: doc.host,
    native: doc.native.map(toPair),
  };
  if (doc.host_version_range !== undefined) caps.hostVersionRange = doc.host_version_range;
  if (doc.close !== undefined) caps.close = doc.close.map(toPair);
  if (doc.synthesized !== undefined) {
    const synthesized: Partial<Record<Event, Recipe>> = {};
    for (const [event, recipe] of Object.entries(doc.synthesized)) {
      // event is a capabilities.yaml map key (runtime string); same trust
      // boundary as toPair's p.event above.
      synthesized[event as Event] = { ...recipe };
    }
    caps.synthesized = synthesized;
  }
  if (doc.unsupported !== undefined) caps.unsupported = doc.unsupported.map((e) => e as Event);

  return caps;
}

/** The support level for e in c, and whether e is classified at all. */
export function level(c: Capabilities, e: Event): { level: Level | ""; ok: boolean } {
  if (c.native.some((p) => p.event === e)) return { level: "native", ok: true };
  if (c.close?.some((p) => p.event === e)) return { level: "close", ok: true };
  if (c.synthesized && e in c.synthesized) return { level: "synthesized", ok: true };
  if (c.unsupported?.includes(e)) return { level: "unsupported", ok: true };
  return { level: "", ok: false };
}

/** The synthesis recipe for e, when e is synthesized. */
export function recipe(c: Capabilities, e: Event): Recipe | undefined {
  return c.synthesized?.[e];
}

/** The host-side event name for a native or close mapping. */
export function hostEvent(c: Capabilities, e: Event): string | undefined {
  return c.native.find((p) => p.event === e)?.hostEvent ?? c.close?.find((p) => p.event === e)?.hostEvent;
}
