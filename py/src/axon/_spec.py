"""The two primitives the Go side exposes through axon.Spec() (an fs.FS
rooted at spec/): read a file, and list a directory. Every other module in
this package reads the spec tree exclusively through these two functions --
never a hard-coded literal drawn from spec/ content itself.

Resolved-path rule: the published wheel has no repository above it, so this
module never reads a repo-relative ``../spec`` at runtime. ``pyproject.toml``
force-includes the repo's ``spec/`` tree into the wheel at ``axon/spec/`` (the
Python analogue of task 3's TypeScript bundling step), so the installed
package's own directory has a ``spec/`` subdirectory sitting right next to
this module. Before a build has run -- i.e. running tests against an
editable install in this checkout, where force-include has not materialized
anything under ``src/axon/`` -- that subdirectory does not exist yet, so we
fall back to the repo-root ``spec/`` tree (three levels up from this file:
``py/src/axon/_spec.py`` -> repo root), which is the same content
force-include would have copied into the wheel. Exactly one of the two
exists in any given environment; whichever is found first wins, and there
is no third location this module will ever look at.
"""

from __future__ import annotations

import os

_PACKAGE_DIR = os.path.dirname(os.path.abspath(__file__))


def _resolve_spec_root() -> str:
    bundled = os.path.join(_PACKAGE_DIR, "spec")
    if os.path.isdir(bundled):
        return bundled

    dev_tree_root = os.path.normpath(os.path.join(_PACKAGE_DIR, "..", "..", "..", "spec"))
    if os.path.isdir(dev_tree_root):
        return dev_tree_root

    raise FileNotFoundError(
        f"axon: spec tree not found at {bundled} or {dev_tree_root} -- "
        "build the wheel to bundle it, or run from a checkout with spec/ at the repo root"
    )


_cached_root: str | None = None


def spec_root() -> str:
    """The resolved spec/ root directory, per the rule documented above."""
    global _cached_root
    if _cached_root is None:
        _cached_root = _resolve_spec_root()
    return _cached_root


def read_spec_file(rel_path: str) -> str:
    """Reads a file's raw text content, relative to the resolved spec root."""
    with open(os.path.join(spec_root(), rel_path), encoding="utf-8") as f:
        return f.read()


def list_spec_dir(rel_path: str) -> list[str]:
    """Lists the entry names of a directory, relative to the resolved spec root."""
    return os.listdir(os.path.join(spec_root(), rel_path))
