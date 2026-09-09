import { existsSync, readFileSync, readdirSync } from "node:fs";
import path from "node:path";

// The two primitives the Go side exposes through axon.Spec() (an fs.FS
// rooted at spec/): read a file, and list a directory. Every other module
// in this package reads the spec tree exclusively through these two
// functions -- never a hard-coded literal drawn from spec/ content itself.
//
// Resolved-path rule: the published npm package has no repo above it, so
// this module never reads `../spec` at runtime. `pnpm build` copies the
// repo's spec/ tree into ts/dist/spec/ (see scripts/bundle-spec.mjs) and
// the compiled dist/spec.js sits right next to dist/spec/, so the bundled
// copy resolves to a directory adjacent to this module's own compiled
// location (`path.join(__dirname, "spec")`). Before a build has run --
// i.e. running tests straight against src/ in this checkout -- dist/spec
// does not exist yet, so we fall back to the repo-root spec/ tree
// (`../../spec` relative to ts/src/), which is the same content bundle-spec
// would have copied. Exactly one of the two exists in any given
// environment; whichever is found first wins, and there is no third
// location this module will ever look at.
function resolveSpecRoot(): string {
  const bundled = path.join(__dirname, "spec");
  if (existsSync(bundled)) return bundled;

  const devTreeRoot = path.resolve(__dirname, "..", "..", "spec");
  if (existsSync(devTreeRoot)) return devTreeRoot;

  throw new Error(
    `axon: spec tree not found at ${bundled} or ${devTreeRoot} -- ` +
      "run `pnpm build` to bundle it, or run from a checkout with spec/ at the repo root",
  );
}

let cachedRoot: string | undefined;

/** The resolved spec/ root directory, per the rule documented above. */
export function specRoot(): string {
  if (cachedRoot === undefined) cachedRoot = resolveSpecRoot();
  return cachedRoot;
}

/** Reads a file's raw text content, relative to the resolved spec root. */
export function readSpecFile(relPath: string): string {
  return readFileSync(path.join(specRoot(), relPath), "utf8");
}

/** Lists the entry names of a directory, relative to the resolved spec root. */
export function listSpecDir(relPath: string): string[] {
  return readdirSync(path.join(specRoot(), relPath));
}
