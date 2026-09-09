// The four contract sentinels, mirroring hop.top/axon's ErrUnknownHost
// (axon package) and ErrUnknownHost / ErrUnsupportedEvent /
// ErrUnsupportedAction / ErrSchema (axon/hooks package). This binding has
// one axon module, so all four live together here.

/** The host name is not one axon knows (canonical names only). */
export class UnknownHostError extends Error {
  constructor(host: string) {
    super(`axon: unknown host: ${host}`);
    this.name = "UnknownHostError";
    Object.setPrototypeOf(this, UnknownHostError.prototype);
  }
}

/**
 * The host has no wire shape for a canonical event -- its capability file
 * marks it unsupported or synthesized, or does not classify it at all.
 * Thrown by the hook codecs (ts/src/hooks/hosts) when encodeInput or
 * encodeDecision is asked to produce a payload for such an event.
 */
export class UnsupportedEventError extends Error {
  constructor(host: string, event: string) {
    super(`axon: event not supported by host: ${host}: ${event}`);
    this.name = "UnsupportedEventError";
    Object.setPrototypeOf(this, UnsupportedEventError.prototype);
  }
}

/**
 * A decision carries an action the host cannot express, or one outside the
 * four canonical action constants. Thrown by the hook codecs
 * (ts/src/hooks/hosts) from encodeDecision -- never silently defaulted to
 * an action the caller did not ask for.
 */
export class UnsupportedActionError extends Error {
  constructor(message: string) {
    super(`axon: action not supported by host: ${message}`);
    this.name = "UnsupportedActionError";
    Object.setPrototypeOf(this, UnsupportedActionError.prototype);
  }
}

/**
 * A payload does not match its schema, a spec file is malformed, or a
 * spec file that must exist for a recognized host (e.g. a hooked host's
 * capabilities.yaml) is missing or unreadable. Distinct from
 * UnknownHostError: SchemaError always implies the host name itself
 * resolved fine -- the failure is about a document, not an identity.
 */
export class SchemaError extends Error {
  constructor(message: string) {
    super(`axon: schema: ${message}`);
    this.name = "SchemaError";
    Object.setPrototypeOf(this, SchemaError.prototype);
  }
}
