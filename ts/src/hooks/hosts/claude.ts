import { loadCapabilities, level, type Capabilities } from "../../capabilities";
import type { Event } from "../../events";
import { get } from "../../registry";
import { UnsupportedActionError, UnsupportedEventError, SchemaError } from "../../errors";
import type { Codec } from "../codec";
import type { Input } from "../input";
import type { Decision } from "../decision";

/**
 * The Claude Code codec. Claude's host-side event names equal the
 * canonical names (its capabilities.yaml is all-native, identity
 * mapping), but the decode map is still built and consulted via the
 * capability reader rather than trusted verbatim -- an unrecognized
 * hook_event_name is ErrUnsupportedEvent, never a canonical Event
 * carrying whatever string the wire held.
 */
class ClaudeCodec implements Codec {
  private readonly caps: Capabilities;
  private readonly hostToCanonical: Map<string, Event>;

  constructor() {
    this.caps = loadCapabilities("claude");
    this.hostToCanonical = decodeMap(this.caps);
  }

  host(): string {
    return "claude";
  }

  capabilities(): Capabilities {
    return this.caps;
  }

  // encodeInput builds Claude's native hook stdin envelope. Requires the
  // event be native or close: a synthesized event has no envelope of its
  // own (it is derived from some other host event by the runtime).
  encodeInput(input: Input): unknown {
    requireNativeOrClose(this.caps, "claude", input.event);

    // extra is written FIRST; the canonical fields below overwrite any
    // colliding key, so a colliding extra key can never clobber the
    // envelope's own discriminator or session id (invariant 4).
    const out: Record<string, unknown> = { ...(input.extra ?? {}) };
    out.hook_event_name = input.event;
    out.session_id = input.sessionId;
    out.cwd = input.cwd;
    switch (input.event) {
      case "PreToolUse":
      case "PermissionRequest":
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

  // decodeInput parses Claude's native hook stdin envelope. The event is
  // looked up in the capability map rather than trusted verbatim.
  decodeInput(raw: unknown): Input {
    const m = asRecord(raw, "claude input");
    const hostEventName = m.hook_event_name;
    if (typeof hostEventName !== "string" || hostEventName === "") {
      throw new SchemaError("claude input: missing hook_event_name");
    }
    const event = this.hostToCanonical.get(hostEventName);
    if (event === undefined) {
      throw new UnsupportedEventError("claude", hostEventName);
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

  // encodeDecision picks the wire shape by decision.event: a blank event
  // or PreToolUse use the tool-gate hookSpecificOutput.permissionDecision
  // shape; PermissionRequest, PostToolUse and SessionStart each have
  // their own native shape. Any other event has no decision channel in
  // Claude's hook contract.
  encodeDecision(d: Decision): { stdout: unknown; exit: number } {
    if (d.event !== undefined && d.event !== "") {
      requireNativeOrClose(this.caps, "claude", d.event);
    }
    const exit = exitFor(this.caps, d.action);
    // `||`, not `??`: Go's `switch d.Event { case "", axon.EventPreToolUse:`
    // treats an explicit empty string exactly like an omitted one. `??`
    // only substitutes on undefined/null and would let an explicit ""
    // fall through to the default/unsupported-event branch.
    switch (d.event || "PreToolUse") {
      case "PreToolUse":
        return { stdout: encodePreToolUseDecision(d), exit };
      case "PermissionRequest":
        return { stdout: encodePermissionRequestDecision(d), exit };
      case "PostToolUse":
        return { stdout: encodeGenericBlockDecision(d, "PostToolUse"), exit };
      case "SessionStart":
        return { stdout: encodeSessionStartDecision(d), exit };
      default:
        throw new UnsupportedEventError("claude", String(d.event));
    }
  }

  // decodeDecision classifies stdout by its recognized decision shape.
  // Empty stdout, or JSON carrying none of Claude's known decision keys,
  // both fall back to actionForExit(exit): the exit code is the only
  // remaining signal (invariant 5).
  decodeDecision(stdout: unknown, exit: number): Decision {
    if (isEmptyStdout(stdout)) {
      return { action: actionForExit(this.caps, exit) };
    }
    const m = asRecord(stdout, "claude decision");
    const { decision, recognized } = decodeKnownShape(m);
    if (!recognized) {
      return { action: actionForExit(this.caps, exit) };
    }
    return decision;
  }
}

// requireNativeOrClose enforces that EncodeInput/EncodeDecision only
// produce a payload the host itself would emit: a synthesized or
// unclassified event has no envelope/decision channel of its own.
function requireNativeOrClose(caps: Capabilities, host: string, event: Event): void {
  const { level: lvl, ok } = level(caps, event);
  if (!ok || (lvl !== "native" && lvl !== "close")) {
    throw new UnsupportedEventError(host, event);
  }
}

// decodeMap builds the host-event -> canonical-event map used to decode a
// native stdin envelope. Native rows always win over close rows
// (invariant 1); two rows within one section sharing a host event is an
// error, never a silent last-row-wins.
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
    if (m.has(p.hostEvent)) continue; // native wins; the close row stays encode-only
    m.set(p.hostEvent, p.event);
  }
  return m;
}

// nativeAction maps canonical actions to Claude's PreToolUse
// permissionDecision. warn has no distinct exit code in Claude's hook
// contract; it still surfaces as the native "ask" permissionDecision.
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
      if (!native) throw new UnsupportedActionError(`claude cannot express action ${String(d.action)}`);
      return {
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          permissionDecision: native,
          permissionDecisionReason: d.message ?? "",
        },
      };
    }
    default:
      throw new UnsupportedActionError(`claude cannot express action ${String(d.action)}`);
  }
}

// encodePermissionRequestDecision: warn has no native channel on this
// event, so it degrades to allow, matching nerv's warn-degrades-to-allow
// rule for PermissionRequest specifically (never to block).
function encodePermissionRequestDecision(d: Decision): unknown {
  switch (d.action) {
    case "allow":
    case "warn":
      return {};
    case "block":
      return { hookSpecificOutput: { hookEventName: "PermissionRequest", decision: { behavior: "deny", message: d.message ?? "" } } };
    case "rewrite":
      return { hookSpecificOutput: { hookEventName: "PermissionRequest", decision: { behavior: "allow", updatedInput: d.rewrite } } };
    default:
      throw new UnsupportedActionError(`claude decision ${String(d.action)} for PermissionRequest`);
  }
}

// encodeGenericBlockDecision is the top-level decision/reason shape
// shared by PostToolUse and (contextless) SessionStart block. Neither
// has a rewrite channel.
function encodeGenericBlockDecision(d: Decision, event: string): unknown {
  switch (d.action) {
    case "allow":
    case "warn":
      return {};
    case "block":
      return { decision: "block", reason: d.message ?? "" };
    default:
      throw new UnsupportedActionError(`claude decision ${String(d.action)} for ${event}`);
  }
}

// encodeSessionStartDecision: a contextless allow is the empty envelope.
// An allow carrying context in metadata emits Claude's native
// additionalContext/systemMessage/continue shape instead (invariant 7).
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
      throw new UnsupportedActionError(`claude decision ${String(d.action)} for SessionStart`);
  }
}

// encodeSessionStartContext builds Claude's SessionStart
// context-injection shape from metadata's three recognized keys, or
// returns undefined when none are set so the caller falls back to `{}`.
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

// decodeKnownShape probes the wire map for Claude's known decision
// shapes and reports whether any were found. event is set whenever the
// wire reveals it.
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
    if (isRecord(hso.decision)) {
      const behavior = hso.decision.behavior;
      if (typeof behavior === "string") {
        recognized = true;
        d.event = "PermissionRequest";
        if (behavior === "deny") {
          d.action = "block";
          d.message = typeof hso.decision.message === "string" ? hso.decision.message : "";
        } else if (behavior === "allow") {
          if ("updatedInput" in hso.decision) {
            d.action = "rewrite";
            d.rewrite = hso.decision.updatedInput;
          } else {
            d.action = "allow";
          }
        }
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

// exitFor maps a canonical action to Claude's process exit code, sourced
// from the host's exitCodes (never a hard-coded literal). warn has no
// distinct exit code in Claude's contract; it exits like allow.
function exitFor(caps: Capabilities, action: Decision["action"]): number {
  const codes = hostExitCodes(caps);
  switch (action) {
    case "block":
      return codes.block;
    case "rewrite":
      return codes.rewrite ?? codes.allow;
    default:
      return codes.allow;
  }
}

// actionForExit is the inverse of exitFor, used to classify decisions
// with no recognized JSON decision shape (including empty stdout) by
// exit code alone.
function actionForExit(caps: Capabilities, exit: number): Decision["action"] {
  const codes = hostExitCodes(caps);
  if (exit === codes.block) return "block";
  if (codes.rewrite !== undefined && exit === codes.rewrite) return "rewrite";
  return "allow";
}

function hostExitCodes(caps: Capabilities): { allow: number; block: number; warn?: number; rewrite?: number } {
  // The claude/codex codecs need the host's exit codes but only hold
  // Capabilities (event mappings), not the Host record itself -- reread
  // it from the registry via the capability reader's own host, never a
  // hard-coded per-host table.
  const h = requireHost(caps.host);
  if (!h.exitCodes) throw new SchemaError(`${caps.host}: no exit_codes in host.yaml`);
  return h.exitCodes;
}

// requireHost/asRecord/isRecord/isEmptyStdout are small shared helpers
// duplicated per host module (matching the Go module's own pattern of
// parallel, not shared, per-host codec logic) -- see the sibling codec
// files for the identical definitions.
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
  return new ClaudeCodec();
}
