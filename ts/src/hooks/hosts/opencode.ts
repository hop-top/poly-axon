import { loadCapabilities, level, hostEvent as capHostEvent, type Capabilities } from "../../capabilities";
import type { Event } from "../../events";
import { UnsupportedActionError, UnsupportedEventError, SchemaError } from "../../errors";
import type { Codec } from "../codec";
import type { Input } from "../input";
import type { Decision } from "../decision";

/**
 * The OpenCode codec.
 *
 * OpenCode hooks are in-process JS/TS plugin callbacks, not subprocesses:
 * there is no exit-code contract (spec/hosts/opencode/host.yaml has no
 * exit_codes key). encodeDecision always returns exit 0; decodeDecision
 * ignores its exit argument and decodes the decision from stdout JSON
 * only (invariant 8).
 *
 * Event names are never hardcoded here: they come from
 * capabilities().hostEvent for encoding and the native-wins decode map
 * for decoding, so spec/hosts/opencode/capabilities.yaml is the single
 * source of the canonical<->dotted event mapping.
 */
class OpencodeCodec implements Codec {
  private readonly caps: Capabilities;
  private readonly rev: Map<string, Event>;

  constructor() {
    this.caps = loadCapabilities("opencode");
    this.rev = decodeMap(this.caps);
  }

  host(): string {
    return "opencode";
  }

  capabilities(): Capabilities {
    return this.caps;
  }

  encodeInput(input: Input): unknown {
    const native = capHostEvent(this.caps, input.event);
    if (native === undefined) {
      throw new UnsupportedEventError("opencode", input.event);
    }

    const out: Record<string, unknown> = { ...(input.extra ?? {}) };
    out.type = native;
    out.session_id = input.sessionId;
    switch (input.event) {
      case "PreToolUse":
      case "PermissionRequest":
        out.tool = input.toolName;
        out.input = input.toolInput;
        break;
      case "PostToolUse":
        out.tool = input.toolName;
        out.input = input.toolInput;
        out.output = input.toolResponse;
        break;
      case "UserPromptSubmit":
        out.prompt = input.prompt;
        break;
      default:
        break;
    }
    return out;
  }

  decodeInput(raw: unknown): Input {
    const m = asRecord(raw, "opencode input");
    const native = m.type;
    if (typeof native !== "string" || native === "") {
      throw new SchemaError("opencode input: missing type");
    }
    const event = this.rev.get(native);
    if (event === undefined) {
      throw new UnsupportedEventError("opencode", native);
    }

    const known = new Set(["type", "session_id", "tool", "input", "output", "prompt"]);
    const extra: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(m)) {
      if (!known.has(k)) extra[k] = v;
    }

    const input: Input = {
      event,
      sessionId: typeof m.session_id === "string" ? m.session_id : "",
      cwd: "",
      extra,
    };
    if (typeof m.tool === "string") input.toolName = m.tool;
    if (isRecord(m.input)) input.toolInput = m.input;
    if (isRecord(m.output)) input.toolResponse = m.output;
    if (typeof m.prompt === "string") input.prompt = m.prompt;
    return input;
  }

  // encodeDecision always returns exit 0 (invariant 8). Warn is passed
  // through verbatim as {"action":"warn","message":...} -- folding it to
  // block would change semantics (block a tool the handler only warned
  // about), so it is never done (invariant 2).
  encodeDecision(d: Decision): { stdout: unknown; exit: number } {
    this.checkEvent(d.event);
    switch (d.action) {
      case "allow":
        return { stdout: { action: "allow" }, exit: 0 };
      case "rewrite":
        return { stdout: { action: "rewrite", rewrite: d.rewrite }, exit: 0 };
      case "warn":
        return { stdout: { action: "warn", message: d.message ?? "" }, exit: 0 };
      case "block":
        return { stdout: { action: "block", message: d.message ?? "" }, exit: 0 };
      default:
        throw new UnsupportedActionError(`opencode cannot express action ${String(d.action)}`);
    }
  }

  // decodeDecision ignores exit entirely: OpenCode decisions live in
  // stdout JSON only. Empty stdout decodes to allow.
  decodeDecision(stdout: unknown, _exit: number): Decision {
    if (isEmptyStdout(stdout)) {
      return { action: "allow" };
    }
    const m = asRecord(stdout, "opencode decision");
    const d: Decision = { action: "allow" };
    switch (m.action) {
      case "block":
        d.action = "block";
        break;
      case "warn":
        d.action = "warn";
        break;
      case "rewrite":
        d.action = "rewrite";
        d.rewrite = m.rewrite;
        break;
      default:
        break;
    }
    d.message = typeof m.message === "string" ? m.message : "";
    return d;
  }

  // checkEvent rejects a decision naming an event OpenCode has no wire
  // shape for. A blank event keeps the host's default shape.
  private checkEvent(ev: Event | undefined): void {
    if (ev === undefined || ev === "") return;
    const { level: lvl, ok } = level(this.caps, ev);
    if (!ok || (lvl !== "native" && lvl !== "close")) {
      throw new UnsupportedEventError("opencode", ev);
    }
  }
}

// decodeMap: native rows always win over close rows. A close row whose
// host event no native row claims is unambiguous and does decode
// (todo.updated -> TaskCompleted and tui.prompt.append -> UserPromptSubmit
// are the only way those envelopes decode at all).
function decodeMap(caps: Capabilities): Map<string, Event> {
  const m = new Map<string, Event>();
  for (const p of caps.native) {
    if (m.has(p.hostEvent)) {
      throw new SchemaError(`${caps.host}: host event ${p.hostEvent} maps to both ${m.get(p.hostEvent)} and ${p.event} under native`);
    }
    m.set(p.hostEvent, p.event);
  }
  const seenClose = new Map<string, Event>();
  for (const p of caps.close ?? []) {
    if (seenClose.has(p.hostEvent)) {
      throw new SchemaError(`${caps.host}: host event ${p.hostEvent} maps to both ${seenClose.get(p.hostEvent)} and ${p.event} under close`);
    }
    seenClose.set(p.hostEvent, p.event);
    if (m.has(p.hostEvent)) continue;
    m.set(p.hostEvent, p.event);
  }
  return m;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function asRecord(v: unknown, what: string): Record<string, unknown> {
  if (!isRecord(v)) throw new SchemaError(`${what}: expected a JSON object`);
  return v;
}

function isEmptyStdout(stdout: unknown): boolean {
  if (stdout === undefined || stdout === null || stdout === "") return true;
  if (typeof stdout === "string") return stdout.trim() === "";
  return false;
}

export function makeCodec(): Codec {
  return new OpencodeCodec();
}
