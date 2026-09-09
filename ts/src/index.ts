// Public entry point of @hop-top/axon (TypeScript binding).

export type { Host, StorePaths, ExitCodes } from "./host";
export { hosts, get, resolve, hookedHosts } from "./registry";

export type { Event, EventInfo, Derivation, PayloadField } from "./events";
export { events, nativeEvents, effectiveOrigin } from "./events";

export type { Capabilities, Pair, Recipe, Level } from "./capabilities";
export { loadCapabilities, level, recipe, hostEvent } from "./capabilities";

export { validate } from "./schema";
export { specRoot, readSpecFile, listSpecDir } from "./spec";

export { UnknownHostError, UnsupportedEventError, UnsupportedActionError, SchemaError } from "./errors";

export type { Action, Input, Decision, Codec } from "./hooks";
export { codecFor, registeredHosts } from "./hooks";
