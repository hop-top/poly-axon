import { loadCapabilities, level, type Capabilities } from "../../capabilities";
import type { Event } from "../../events";
import { get } from "../../registry";
import { UnsupportedActionError, UnsupportedEventError, SchemaError } from "../../errors";
import type { Codec } from "../codec";
import type { Input } from "../input";
import type { Decision } from "../decision";

/**
 * The Codex CLI codec. Codex's handler contract mirrors Claude Code's:
 * stdin JSON envelope, stdout JSON decision, exit code. Codex's host-side
 * event names also equal the canonical names.
 */
class CodexCodec implements Codec {
  private readonly caps: Capabilities;
  private readonly hostToCanonical: Map<string, Event>;

  constructor() {
    this.caps = loadCapabilities("codex");
    this.hostToCanonical = decodeMap(this.caps);
  }

  host(): string {
    return "codex";
  }

  capabilities(): Capabilities {
    return this.caps;
  }

  encodeInput(input: Input): unknown {
    requireNativeOrClose(this.caps, "codex", input.event);

    const out: Record<string, unknown> = { ...(input.extra ?? {}) };
    out.hook_event_name = input.event;
    out.session_id = input.sessionId;
    out.cwd = input.cwd;
    switch (input.event) {
      case "PreToolUse":
        out.tool_name = input.toolName;
        out.tool_input = input.toolInput;
        break;
      case "PostToolUse":
        out.tool_name = input.toolName;
        out.tool_input = input.toolInput;
        out.tool_response = input.toolResponse;
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
    const m = asRecord(raw, "codex input");
    const hostEventName = m.hook_event_name;
    if (typeof hostEventName !== "string" || hostEventName === "") {
      throw new SchemaError("codex input: missing hook_event_name");
    }
    const event = this.hostToCanonical.get(hostEventName);
    if (event === undefined) {
      throw new UnsupportedEventError("codex", hostEventName);
    }

    const known = new Set(["hook_event_name", "session_id", "cwd", "tool_name", "tool_input", "tool_response", "prompt"]);
    const extra: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(m)) {
      if (!known.has(k)) extra[k] = v;
    }

    const input: Input = {
      event,
      sessionId: typeof m.session_id === "string" ? m.session_id : "",
      cwd: typeof m.cwd === "string" ? m.cwd : "",
      extra,
    };
    if (typeof m.tool_name === "string") input.toolName = m.tool_name;
    if (isRecord(m.tool_input)) input.toolInput = m.tool_input;
    if (isRecord(m.tool_response)) input.toolResponse = m.tool_response;
    if (typeof m.prompt === "string") input.prompt = m.prompt;
    return input;
  }

  // encodeDecision mirrors claude's shape switch: a blank event or
  // PreToolUse use the tool-gate shape; PostToolUse uses the top-level
  // decision/reason shape; SessionStart allows with the empty envelope
  // (or context metadata) and blocks with the same top-level shape.
  // UserPromptSubmit and Stop allow with the empty envelope; codex
  // defines no dedicated block shape for either, so block on those two
  // is unsupported, same as claude.
  encodeDecision(d: Decision): { stdout: unknown; exit: number } {
    if (d.event !== undefined && d.event !== "") {
      requireNativeOrClose(this.caps, "codex", d.event);
    }
    const exit = exitFor(this.caps, d.action);
    // `||`, not `??`: Go's `switch d.Event { case "", axon.EventPreToolUse:`
    // treats an explicit empty string exactly like an omitted one. `??`
    // only substitutes on undefined/null and would let an explicit ""
    // fall through to the default/unsupported-event branch.
    switch (d.event || "PreToolUse") {
      case "PreToolUse":
        return { stdout: encodePreToolUseDecision(d), exit };
      case "PostToolUse":
        return { stdout: encodeGenericBlockDecision(d, "PostToolUse"), exit };
      case "SessionStart":
        return { stdout: encodeSessionStartDecision(d), exit };
      case "UserPromptSubmit":
      case "Stop":
        if (d.action === "allow" || d.action === "warn") return { stdout: {}, exit };
        throw new UnsupportedActionError(`codex decision ${String(d.action)} for ${d.event}`);
      default:
        throw new UnsupportedEventError("codex", String(d.event));
    }
  }

  decodeDecision(stdout: unknown, exit: number): Decision {
    if (isEmptyStdout(stdout)) {
      return { action: actionForExit(this.caps, exit) };
    }
    const m = asRecord(stdout, "codex decision");
    const { decision, recognized } = decodeKnownShape(m);
    if (!recognized) {
      return { action: actionForExit(this.caps, exit) };
    }
    return decision;
  }
}

function requireNativeOrClose(caps: Capabilities, host: string, event: Event): void {
  const { level: lvl, ok } = level(caps, event);
  if (!ok || (lvl !== "native" && lvl !== "close")) {
    throw new UnsupportedEventError(host, event);
  }
}

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

const NATIVE_ACTION: Partial<Record<Decision["action"], string>> = { allow: "allow", warn: "ask", block: "deny" };

function encodePreToolUseDecision(d: Decision): unknown {
  switch (d.action) {
    case "allow":
      return {};
    case "rewrite":
      return { hookSpecificOutput: { hookEventName: "PreToolUse", permissionDecision: "allow", updatedInput: d.rewrite } };
    case "block":
    case "warn": {
      const native = NATIVE_ACTION[d.action];
      if (!native) throw new UnsupportedActionError(`codex cannot express action ${String(d.action)}`);
      return {
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          permissionDecision: native,
          permissionDecisionReason: d.message ?? "",
        },
      };
    }
    default:
      throw new UnsupportedActionError(`codex cannot express action ${String(d.action)}`);
  }
}

function encodeGenericBlockDecision(d: Decision, event: string): unknown {
  switch (d.action) {
    case "allow":
    case "warn":
      return {};
    case "block":
      return { decision: "block", reason: d.message ?? "" };
    default:
      throw new UnsupportedActionError(`codex decision ${String(d.action)} for ${event}`);
  }
}

function encodeSessionStartDecision(d: Decision): unknown {
  switch (d.action) {
    case "allow":
    case "warn": {
      const withContext = encodeSessionStartContext(d.metadata);
      return withContext ?? {};
    }
    case "block":
      return { decision: "block", reason: d.message ?? "" };
    default:
      throw new UnsupportedActionError(`codex decision ${String(d.action)} for SessionStart`);
  }
}

function encodeSessionStartContext(metadata: Record<string, unknown> | undefined): Record<string, unknown> | undefined {
  if (!metadata || Object.keys(metadata).length === 0) return undefined;
  const out: Record<string, unknown> = {};
  if (typeof metadata.additional_context === "string" && metadata.additional_context !== "") {
    out.hookSpecificOutput = { additionalContext: metadata.additional_context };
  }
  if (typeof metadata.system_message === "string" && metadata.system_message !== "") {
    out.systemMessage = metadata.system_message;
  }
  if (typeof metadata.continue === "boolean") {
    out.continue = metadata.continue;
  }
  return Object.keys(out).length === 0 ? undefined : out;
}

// decodeKnownShape mirrors claude's, minus the PermissionRequest branch:
// codex has no PermissionRequest decision channel.
function decodeKnownShape(m: Record<string, unknown>): { decision: Decision; recognized: boolean } {
  const d: Decision = { action: "allow" };
  let recognized = false;

  const hso = isRecord(m.hookSpecificOutput) ? m.hookSpecificOutput : undefined;
  if (hso) {
    if (typeof hso.hookEventName === "string" && hso.hookEventName !== "") {
      // hookEventName is untrusted wire data (JSON from the host
      // process), not a value this codec constructs -- same trust
      // boundary as Event's other wire-loaded assignments (events.ts,
      // capabilities.ts). Type annotation only; the runtime value and
      // control flow are unchanged.
      d.event = hso.hookEventName as Event;
    }
    if ("permissionDecision" in hso) {
      recognized = true;
      if (d.event === undefined) d.event = "PreToolUse";
      const pd = hso.permissionDecision;
      if (pd === "deny") d.action = "block";
      else if (pd === "ask") d.action = "warn";
      d.message = typeof hso.permissionDecisionReason === "string" ? hso.permissionDecisionReason : "";
      if ("updatedInput" in hso) {
        d.action = "rewrite";
        d.rewrite = hso.updatedInput;
      }
    }
    if (typeof hso.additionalContext === "string" && hso.additionalContext !== "") {
      recognized = true;
      d.event = "SessionStart";
      d.action = "allow";
      setMetadata(d, "additional_context", hso.additionalContext);
    }
  }
  if (typeof m.systemMessage === "string" && m.systemMessage !== "") {
    recognized = true;
    d.event = "SessionStart";
    setMetadata(d, "system_message", m.systemMessage);
  }
  if (typeof m.continue === "boolean") {
    recognized = true;
    d.event = "SessionStart";
    setMetadata(d, "continue", m.continue);
  }
  if (m.decision === "block") {
    recognized = true;
    d.action = "block";
    d.message = typeof m.reason === "string" ? m.reason : "";
  }
  return { decision: d, recognized };
}

function setMetadata(d: Decision, key: string, value: unknown): void {
  if (!d.metadata) d.metadata = {};
  d.metadata[key] = value;
}

function exitFor(caps: Capabilities, action: Decision["action"]): number {
  const codes = hostExitCodes(caps);
  switch (action) {
    case "allow":
      return codes.allow;
    case "warn":
      return codes.warn ?? codes.allow;
    case "block":
      return codes.block;
    case "rewrite":
      return codes.rewrite ?? codes.allow;
    default:
      return codes.allow;
  }
}

// actionForExit: codex's host.yaml (like claude's) leaves warn unset,
// defaulting it to the same value as allow, so an unset warn code can
// never win a match here unless it is explicitly declared and distinct.
function actionForExit(caps: Capabilities, exit: number): Decision["action"] {
  const codes = hostExitCodes(caps);
  if (exit === codes.block) return "block";
  if (codes.rewrite !== undefined && exit === codes.rewrite) return "rewrite";
  if (codes.warn !== undefined && codes.warn !== codes.allow && exit === codes.warn) return "warn";
  return "allow";
}

function hostExitCodes(caps: Capabilities): { allow: number; block: number; warn?: number; rewrite?: number } {
  const h = requireHost(caps.host);
  if (!h.exitCodes) throw new SchemaError(`${caps.host}: no exit_codes in host.yaml`);
  return h.exitCodes;
}

function requireHost(name: string) {
  const h = get(name);
  if (!h) throw new SchemaError(`host ${name} not found`);
  return h;
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
  return new CodexCodec();
}
