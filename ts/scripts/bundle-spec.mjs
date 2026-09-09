#!/usr/bin/env node
// Copies the repo's spec/ tree into ts/dist/spec/ as part of `pnpm build`.
//
// The published npm package has no repo above it, so spec.ts must never
// read `../spec` at runtime -- it resolves the bundled copy relative to
// its own compiled module location (see ts/src/spec.ts). This script is
// the one place that reaches outside ts/ (to repo-root spec/); it runs
// only at build time, in this checkout, never in a consumer's install.
//
// The tree is ~560 KB across ~128 files, so a plain recursive copy is
// cheap -- no need for a manifest, hashing, or incremental copy.

import { cpSync, existsSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const tsRoot = path.resolve(here, "..");
const repoRoot = path.resolve(tsRoot, "..");

const src = path.join(repoRoot, "spec");
const dest = path.join(tsRoot, "dist", "spec");

if (!existsSync(src)) {
  console.error(`bundle-spec: source tree not found: ${src}`);
  process.exit(1);
}

rmSync(dest, { recursive: true, force: true });
cpSync(src, dest, { recursive: true });

console.log(`bundle-spec: copied ${src} -> ${dest}`);
