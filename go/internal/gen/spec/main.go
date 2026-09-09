// Command gen-spec mirrors the repo-root spec/ tree into go/spec/.
//
// The root spec/ tree is the canonical, language-neutral source of truth;
// go/spec/ is a generated, committed copy of it. Never hand-edit go/spec/.
//
// The copy is committed rather than produced at build time because
// //go:embed cannot traverse upward out of the module directory, and a
// consumer running `go get hop.top/axon` never executes this repo's
// Makefile: the embedded tree has to be present in the published module.
// `make generate-check` gates the resulting duplication against drift.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

func main() {
	root := moduleRoot()
	in := flag.String("in", filepath.Join(filepath.Dir(root), "spec"), "canonical spec directory")
	out := flag.String("out", filepath.Join(root, "spec"), "generated mirror directory")
	check := flag.Bool("check", false, "exit 1 if the mirror differs from the canonical tree")
	flag.Parse()

	src, err := treeOf(*in)
	if err != nil {
		fatal(fmt.Errorf("read canonical tree %s: %w", *in, err))
	}
	if len(src) == 0 {
		fatal(fmt.Errorf("canonical tree %s holds no files", *in))
	}
	if err := plausible(*in, src); err != nil {
		fatal(err)
	}

	if *check {
		dst, err := treeOf(*out)
		if err != nil {
			fatal(fmt.Errorf("read mirror %s: %w", *out, err))
		}
		if !report(*out, src, dst) {
			os.Exit(1)
		}
		return
	}

	if err := write(*out, src); err != nil {
		fatal(err)
	}
}

// plausible rejects a tree that does not look like the canonical spec.
//
// Without this, running `go generate ./...` inside a PUBLISHED module
// destroys the embedded tree. `go generate` sets cwd to the module root,
// so gen-invoke's `-spec-dir ../spec/hosts` resolves outside the module
// and manufactures a sibling spec/hosts holding only invoke.yaml files.
// gen-spec would then read that 11-file directory as canonical and prune
// the committed 128-file mirror to match -- silently, exit 0. A consumer
// has no root spec/ above the module, so the only safe response is to
// refuse a tree missing the files every real spec/ root carries.
func plausible(dir string, tree map[string][]byte) error {
	for _, want := range []string{"version.yaml", "events.yaml", "host.schema.json"} {
		if _, ok := tree[want]; !ok {
			return fmt.Errorf(
				"canonical tree %s is missing %s, so it is not a spec root; "+
					"gen-spec only runs against the repo-root spec/ tree, never inside a published module",
				dir, want)
		}
	}
	return nil
}

// treeOf reads every regular file under dir, keyed by slash-separated
// path relative to dir. A missing dir is an error; the caller decides
// whether that is fatal or a reportable difference.
func treeOf(dir string) (map[string][]byte, error) {
	tree := make(map[string][]byte)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // path produced by walking the flag-supplied tree
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tree, nil
}

// report prints every difference between the canonical tree and the
// mirror and returns true only when they match exactly. All three drift
// classes are named: a file present in src but not dst is missing, one
// present in dst but not src is extra, and one present in both with
// different bytes is stale.
func report(outDir string, src, dst map[string][]byte) bool {
	ok := true
	for _, rel := range sortedKeys(src) {
		want := src[rel]
		got, present := dst[rel]
		switch {
		case !present:
			fmt.Fprintln(os.Stderr, filepath.Join(outDir, rel)+" is missing; run go generate ./...")
			ok = false
		case !bytes.Equal(got, want):
			fmt.Fprintln(os.Stderr, filepath.Join(outDir, rel)+" is stale; run go generate ./...")
			ok = false
		}
	}
	for _, rel := range sortedKeys(dst) {
		if _, present := src[rel]; !present {
			fmt.Fprintln(os.Stderr, filepath.Join(outDir, rel)+" is not in the canonical spec tree; run go generate ./...")
			ok = false
		}
	}
	return ok
}

// write replaces outDir with exactly the files in src, removing any file
// or directory the canonical tree no longer has.
func write(outDir string, src map[string][]byte) error {
	if err := prune(outDir, src); err != nil {
		return err
	}
	for _, rel := range sortedKeys(src) {
		path := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // generated spec directory, meant to be world-readable like the repo's checked-in tree
			return err
		}
		if err := os.WriteFile(path, src[rel], 0o644); err != nil { //nolint:gosec // generated spec file, meant to be world-readable like the repo's checked-in tree
			return err
		}
	}
	return nil
}

// prune deletes files under outDir that src does not have, then removes
// the directories left empty. Without it a file deleted from the
// canonical tree would survive in the mirror forever.
func prune(outDir string, src map[string][]byte) error {
	dst, err := treeOf(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, rel := range sortedKeys(dst) {
		if _, keep := src[rel]; keep {
			continue
		}
		if err := os.Remove(filepath.Join(outDir, rel)); err != nil {
			return err
		}
	}
	return pruneEmptyDirs(outDir)
}

// pruneEmptyDirs removes empty directories bottom-up, leaving outDir
// itself in place.
func pruneEmptyDirs(outDir string) error {
	var dirs []string
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != outDir {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Deepest first, so a directory emptied by removing its children is
	// itself removable in the same pass.
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			continue
		}
		if err := os.Remove(dir); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// moduleRoot returns the Go module root, resolved from this source
// file's compiled-in path rather than the working directory, so the
// generator locates the canonical tree identically whether it is run by
// `go generate` from the module root or by `go run` from anywhere else.
func moduleRoot() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		fatal(fmt.Errorf("runtime.Caller failed; cannot locate the module root"))
	}
	// this file is <root>/internal/gen/spec/main.go
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "gen-spec:", err); os.Exit(1) }
