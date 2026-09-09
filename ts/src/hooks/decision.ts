import type { Event } from "../events";
import type { Action } from "./input";

/**
 * The canonical, host-independent shape of a hook handler's result. A
 * Codec encodes it into a host's native stdout/exit-code convention and
 * decodes that convention back into a Decision.
 */
export interface Decision {
  /**
   * The hook event this decision answers. Codecs whose wire shape varies
   * by event use it on encode; a blank/absent event means the host's
   * default tool-gate shape (PreToolUse). Decoders set it when the wire
   * reveals the event, otherwise leave it unset.
   */
  event?: Event;
  action: Action;
  message?: string;
  rewrite?: unknown;
  metadata?: Record<string, unknown>;
}
