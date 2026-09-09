package axon

import "errors"

// ErrUnknownHost is returned when an operation is asked to act on a host
// name that Get cannot resolve.
var ErrUnknownHost = errors.New("axon: unknown host")
