import YAML from "yaml";
import { readSpecFile } from "./spec";
import { validate } from "./schema";
import type { CanonicalEvent } from "./events_gen";

export type { CanonicalEvent } from "./events_gen";
export { allCanonicalEvents } from "./events_gen";

/**
 * A canonical hook event name from spec/events.yaml, or "". Event names
 * are the wire vocabulary (contract notes §7) -- never normalized, cased,
 * or rewritten by this binding.
 *
 * The literal union comes from CanonicalEvent (generated from
 * spec/events.yaml by ts/scripts/gen-events.mjs -- see events_gen.ts) so a
 * typo'd event name is a compile error, matching axon.Event's generated
 * constants on the Go side (events_gen.go). "" is added by hand here,
 * deliberately not generated: Go's Event is `type Event string`, an open
 * named-string type, so its zero value Event("") is valid Go with no
 * catalog entry of its own; a closed TypeScript literal union has no
 * equivalent zero value unless one is named explicitly. A blank event is
 * meaningful on Decision (docs/spec-contract-notes.md and the four host
 * codecs: "blank means the host's default tool-gate shape", pinned by
 * `encodeDecision({ event: "" })` tests on every hooked host) and must
 * keep type-checking as an Event, so "" joins the union rather than being
 * modeled purely through optionality.
 */
export type Event = CanonicalEvent | "";

/** How a derived event is synthesized from a native one. */
export interface Derivation {
  from: Event;
  when: Record<string, unknown>;
}

/** One payload field entry in an event's catalog description. */
export interface PayloadField {
  field: string;
  type: string;
  notes?: string;
  example?: string;
  required?: boolean;
  format?: string;
  description?: string;
  enum?: string[];
}

/** One events.yaml catalog entry. */
export interface EventInfo {
  name: Event;
  category: string;
  description: string;
  direction: string;
  blocking: boolean;
  payload?: PayloadField[];
  /** "native" | "extension" | "derived" | undefined (absent means native -- see effectiveOrigin). */
  origin?: string;
  extensionSource?: string;
  derivation?: Derivation;
}

/** Returns info.origin, or "native" when absent. */
export function effectiveOrigin(info: EventInfo): string {
  return info.origin ?? "native";
}

interface RawPayloadField {
  field: string;
  type: string;
  notes?: string;
  example?: string;
  required?: boolean;
  format?: string;
  description?: string;
  enum?: string[];
}

interface RawEvent {
  name: string;
  category: string;
  description: string;
  direction: string;
  blocking: boolean;
  payload?: RawPayloadField[];
  origin?: string;
  extension_source?: string;
  derivation?: { from: string; when: Record<string, unknown> };
}

let cachedEvents: EventInfo[] | undefined;

function loadEvents(): EventInfo[] {
  const raw = readSpecFile("events.yaml");
  const doc = YAML.parse(raw) as { events: RawEvent[] };
  validate("events.yaml", "events.schema.json", doc);

  return doc.events.map((e) => {
    // e.name/e.derivation.from are read from spec/events.yaml at runtime;
    // the JSON schema constrains them to `string`, not to the closed
    // Event union (a 33rd catalog entry is exactly what generate-check
    // exists to catch -- see ts/scripts/gen-events.mjs -- not something
    // this loader can enforce statically). The cast documents that trust
    // boundary rather than hiding it behind `as Event` scattered per use.
    const name = e.name as Event;
    const info: EventInfo = {
      name,
      category: e.category,
      description: e.description,
      direction: e.direction,
      blocking: e.blocking,
    };
    if (e.payload !== undefined) info.payload = e.payload;
    if (e.origin !== undefined) info.origin = e.origin;
    if (e.extension_source !== undefined) info.extensionSource = e.extension_source;
    if (e.derivation !== undefined) {
      info.derivation = { from: e.derivation.from as Event, when: e.derivation.when };
    }
    return info;
  });
}

/** The full catalog, in file order. */
export function events(): EventInfo[] {
  if (cachedEvents === undefined) cachedEvents = loadEvents();
  return cachedEvents;
}

/**
 * The host-contract scope: events a host CLI can emit directly. Origin is
 * the whole test, matching Go's NativeEvents() (contract notes §2c): an
 * extension event is native to one CLI but not all, and a derived event is
 * synthesized rather than emitted, so both are excluded. 26 of the 32
 * catalog entries qualify.
 */
export function nativeEvents(): Event[] {
  return events()
    .filter((e) => effectiveOrigin(e) === "native")
    .map((e) => e.name);
}
