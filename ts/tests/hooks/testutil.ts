import { readFileSync } from "node:fs";
import path from "node:path";
import { specRoot } from "../../src/index";

/** Reads and parses a golden fixture for `host`, by file name (e.g. "PreToolUse.block.json"). */
export function fixture(host: string, name: string): unknown {
  const raw = readFileSync(path.join(specRoot(), "fixtures", "hosts", host, name), "utf8");
  return JSON.parse(raw);
}
