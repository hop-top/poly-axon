package axon

import (
	"embed"
	"io/fs"
)

//go:embed spec
var specFS embed.FS

// Spec returns the embedded spec tree rooted at spec/. Bindings in other
// languages read the same files; Go tests read them through this.
func Spec() fs.FS {
	sub, err := fs.Sub(specFS, "spec")
	if err != nil {
		panic("axon: embedded spec missing: " + err.Error())
	}
	return sub
}
