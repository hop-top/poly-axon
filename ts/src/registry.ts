import { listSpecDir } from "./spec";
import { loadHostFile, type Host } from "./host";

interface Registry {
  byName: Map<string, Host>;
  aliasIndex: Map<string, string>;
}

let cached: Registry | undefined;

function loadRegistry(): Registry {
  const byName = new Map<string, Host>();
  const aliasIndex = new Map<string, string>();

  for (const entry of listSpecDir("hosts")) {
    const host = loadHostFile(`hosts/${entry}/host.yaml`);
    byName.set(host.name, host);
    for (const alias of host.aliases) {
      aliasIndex.set(alias, host.name);
    }
  }

  return { byName, aliasIndex };
}

function registry(): Registry {
  if (cached === undefined) cached = loadRegistry();
  return cached;
}

/** Every host, sorted by canonical name. */
export function hosts(): Host[] {
  return [...registry().byName.values()].sort((a, b) => a.name.localeCompare(b.name));
}

/** Looks up a host by canonical name only -- never an alias. */
export function get(name: string): Host | undefined {
  return registry().byName.get(name);
}

/**
 * Accepts a canonical name or a published alias. The only alias-aware
 * function in this module.
 */
export function resolve(nameOrAlias: string): Host | undefined {
  const direct = registry().byName.get(nameOrAlias);
  if (direct) return direct;
  const canonical = registry().aliasIndex.get(nameOrAlias);
  return canonical === undefined ? undefined : registry().byName.get(canonical);
}

/** Hosts with a hook surface (hooks: true). */
export function hookedHosts(): Host[] {
  return hosts().filter((h) => h.hooks);
}
