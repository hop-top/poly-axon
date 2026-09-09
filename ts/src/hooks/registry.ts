import type { Codec } from "./codec";

// Mirrors hop.top/axon/hooks's Register/For/Registered: a process-wide map
// from canonical host name to its codec, populated by each host module's
// own side-effecting registration (see hosts/all.ts) rather than a
// hard-coded table here.
const codecs = new Map<string, Codec>();

/**
 * Adds a codec to the registry. Throws on a duplicate host -- a
 * programming error caught at load time, mirroring Go's Register panic.
 */
export function register(c: Codec): void {
  const host = c.host();
  if (codecs.has(host)) {
    throw new Error(`axon/hooks: codec for ${host} registered twice`);
  }
  codecs.set(host, c);
}

/** Returns the codec for a canonical host name, or undefined if none is registered. */
export function codecFor(host: string): Codec | undefined {
  return codecs.get(host);
}

/** The sorted list of hosts with a registered codec. */
export function registeredHosts(): string[] {
  return [...codecs.keys()].sort((a, b) => a.localeCompare(b));
}
