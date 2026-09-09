import type { Capabilities } from "../capabilities";
import type { Input } from "./input";
import type { Decision } from "./decision";

/** Translates between a host's native hook wire format and the canonical Input/Decision types. */
export interface Codec {
  host(): string;
  /** Builds the host's native hook stdin envelope, as a JSON value. */
  encodeInput(input: Input): unknown;
  /** Parses a host's native hook stdin envelope (already-parsed JSON value). */
  decodeInput(raw: unknown): Input;
  /** Encodes a Decision into the host's native stdout/exit-code convention. */
  encodeDecision(d: Decision): { stdout: unknown; exit: number };
  /** Decodes a host's stdout/exit-code convention back into a Decision. */
  decodeDecision(stdout: unknown, exit: number): Decision;
  capabilities(): Capabilities;
}
