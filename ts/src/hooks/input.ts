import type { Event } from "../events";

/** The canonical outcome a hook handler returns for an event. */
export type Action = "allow" | "warn" | "block" | "rewrite";

/**
 * The canonical, host-independent shape of a hook invocation's input. A
 * Codec translates a host's native payload into an Input and back; fields
 * the canonical set does not name land in `extra`.
 */
export interface Input {
  event: Event;
  sessionId: string;
  cwd: string;
  toolName?: string;
  toolInput?: Record<string, unknown>;
  toolResponse?: Record<string, unknown>;
  prompt?: string;
  extra?: Record<string, unknown>;
}
