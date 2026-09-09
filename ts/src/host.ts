import YAML from "yaml";
import { readSpecFile } from "./spec";
import { validate } from "./schema";

/** XDG-style root paths for a host's persistent stores. */
export interface StorePaths {
  config?: string;
  data?: string;
  cache?: string;
  state?: string;
}

/**
 * A host's hook decision actions mapped to process exit codes. Absent
 * entirely on a host with no exit-code contract (see contract notes §2a,
 * e.g. OpenCode: hooks run in-process and signal block by throwing, not by
 * process exit code). Never defaulted to zeros -- a binding that reads
 * zeros for an absent contract would silently treat "no contract" as
 * "exit 0 means allow".
 */
export interface ExitCodes {
  allow: number;
  block: number;
  warn?: number;
  rewrite?: number;
  error?: number;
}

/** One axon-identified coding-assistant CLI, decoded from hosts/<name>/host.yaml. */
export interface Host {
  name: string;
  status: string;
  aliases: string[];
  binaries: string[];
  storeRoots: StorePaths;
  configFilePatterns: string[];
  projectKeyStrategy: string;
  hookConfigPaths: string[];
  exitCodes?: ExitCodes;
  envelopeDiscriminator: string;
  hooks: boolean;
}

interface RawHost {
  name: string;
  status: string;
  aliases?: string[];
  binaries: string[];
  store_roots?: { config?: string; data?: string; cache?: string; state?: string };
  config_file_patterns?: string[];
  project_key_strategy?: string;
  hook_config_paths?: string[];
  exit_codes?: { allow: number; block: number; warn?: number; rewrite?: number; error?: number };
  envelope_discriminator?: string;
  hooks?: boolean;
}

/**
 * Reads and validates one host.yaml (relative to the spec root, e.g.
 * "hosts/claude/host.yaml") and maps it onto the Host shape. `exitCodes`
 * is copied only when the source document has an `exit_codes` key present
 * -- checked by presence in the parsed YAML value, never defaulted -- per
 * contract notes §2a.
 */
export function loadHostFile(relPath: string): Host {
  const raw = readSpecFile(relPath);
  const doc = YAML.parse(raw) as RawHost;
  validate(relPath, "host.schema.json", doc);

  const host: Host = {
    name: doc.name,
    status: doc.status,
    aliases: doc.aliases ?? [],
    binaries: doc.binaries,
    storeRoots: {
      config: doc.store_roots?.config,
      data: doc.store_roots?.data,
      cache: doc.store_roots?.cache,
      state: doc.store_roots?.state,
    },
    configFilePatterns: doc.config_file_patterns ?? [],
    projectKeyStrategy: doc.project_key_strategy ?? "",
    hookConfigPaths: doc.hook_config_paths ?? [],
    envelopeDiscriminator: doc.envelope_discriminator ?? "",
    hooks: doc.hooks ?? false,
  };

  if (doc.exit_codes !== undefined) {
    host.exitCodes = { ...doc.exit_codes };
  }

  return host;
}
