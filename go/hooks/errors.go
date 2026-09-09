package hooks

import "errors"

var (
	// ErrUnknownHost: the host name is not one axon knows.
	ErrUnknownHost = errors.New("axon/hooks: unknown host")
	// ErrUnsupportedEvent: the host has no wire shape for this canonical
	// event — its capability file marks it unsupported or synthesized, or
	// does not classify it at all.
	ErrUnsupportedEvent = errors.New("axon/hooks: event not supported by host")
	// ErrUnsupportedAction: the Decision carries an Action the host cannot
	// express, or one outside the four canonical Action constants. Codecs
	// return this instead of falling back to a default shape, so a wrong
	// value never reaches the wire with a nil error.
	ErrUnsupportedAction = errors.New("axon/hooks: action not supported by host")
	// ErrSchema: a payload does not match its schema, or a spec file is
	// malformed.
	ErrSchema = errors.New("axon/hooks: schema")
)
