// The hooks surface: canonical Input/Decision types, the Codec interface,
// and the registry lookup. Importing "./hosts/all" registers the four
// codecs this task implements as a side effect.
import "./hosts/all";

export type { Action, Input } from "./input";
export type { Decision } from "./decision";
export type { Codec } from "./codec";
export { codecFor, registeredHosts } from "./registry";
