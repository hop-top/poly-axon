package axon

// Every generator reads the CANONICAL tree at ../spec, never the generated
// go/spec mirror. Reading the mirror makes one `go generate` pass
// insufficient: gen-spec refreshes the mirror last, so the earlier
// generators would consume the previous revision's data and emit stale
// constants. generate-check catches that, but only after the fact.
//
// gen-spec still runs last, because it mirrors what gen-invoke writes into
// the canonical tree.
//go:generate go run ./internal/gen/events -in ../spec/events.yaml
//go:generate go run ./internal/gen/hosts -in ../spec/hosts
//go:generate go run ./internal/gen/invoke -spec-dir ../spec/hosts
//go:generate go run ./internal/gen/spec
