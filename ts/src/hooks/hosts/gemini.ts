import { loadCapabilities, level, hostEvent as capHostEvent, type Capabilities } from "../../capabilities";
import type { Event } from "../../events";
import { get } from "../../registry";
import { UnsupportedActionError, UnsupportedEventError, SchemaError } from "../../errors";
import type { Codec } from "../codec";
import type { Input } from "../input";
import type { Decision } from "../decision";

/** The Gemini CLI codec. */
class GeminiCodec implements Codec {
  private readonly caps: Capabilities;
  private readonly hostToCanonical: Map<string, Event>;

  constructor() {
    this.caps = loadCapabilities("gemini");
    this.hostToCanonical = decodeMap(this.caps);
  }

  host(): string {
    return "gemini";
  }

  capabilities(): Capabilities {
    return this.caps;
  }

  // encodeInput fails with UnsupportedEventError when the event has no
  // native or close mapping: synthesized, unsupported, or unclassified
  // events cannot be encoded as a Gemini-native payload since Gemini
  // itself never emits them.
  encodeInput(input: Input): unknown {
    requireNativeOrClose(this.caps, "gemini", input.event);
    const hostEventName = capHostEvent(this.caps, input.event);
    if (hostEventName === undefined) {
      throw new UnsupportedEventError("gemini", input.event);
    }

    const out: Record<string, unknown> = { ...(input.extra ?? {}) };
    out.hook_event_name = hostEventName;
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

  // decodeInput looks up the canonical event in the native-wins decode
  // map on hook_event_name; a name not there is SchemaError, never a
  // silent fallback.
  decodeInput(raw: unknown): Input {
    const m = asRecord(raw, "gemini input");
    const hostEventName = m.hook_event_name;
    if (typeof hostEventName !== "string" || hostEventName === "") {
      throw new SchemaError("gemini input: missing hook_event_name");
    }
    const event = this.hostToCanonical.get(hostEventName);
    if (event === undefined) {
      throw new UnsupportedEventError("gemini", hostEventName);
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

  // encodeDecision emits Gemini's native decision envelope. Gemini has no
  // native "warn"; nerv folds warn into decision "allow" and, when the
  // message is non-empty, carries it in BOTH "reason" and "systemMessage"
  // (invariant 2: never mapped to block). Gemini does not vary its wire
  // shape by event (invariant 6): the event gate below only rejects an
  // event Gemini cannot express at all; it does not change the shape.
  encodeDecision(d: Decision): { stdout: unknown; exit: number } {
    this.checkEvent(d.event);
    const codes = hostExitCodes(this.caps);
    switch (d.action) {
      case "allow":
        return { stdout: { decision: "allow" }, exit: codes.allow };
      case "warn": {
        const out: Record<string, unknown> = { decision: "allow" };
        if (d.message) {
          out.reason = d.message;
          out.systemMessage = d.message;
        }
        // host.yaml carries no warn exit code for gemini; warn exits like allow.
        return { stdout: out, exit: codes.allow };
      }
      case "rewrite":
        // host.yaml carries no rewrite exit code for gemini; rewrite
        // still allows the tool call to proceed, so it exits like allow.
        return {
          stdout: { decision: "allow", hookSpecificOutput: { tool_input: d.rewrite } },
          exit: codes.allow,
        };
      case "block":
        return { stdout: { decision: "deny", reason: d.message ?? "" }, exit: codes.block };
      default:
        throw new UnsupportedActionError(`gemini cannot express action ${String(d.action)}`);
    }
  }

  // decodeDecision reads Gemini's native decision envelope. Empty stdout,
  // or JSON that carries none of Gemini's known decision keys, both fall
  // back to actionForExit(exit): the exit code is the only remaining
  // signal. Without this gate, unrecognized JSON (including a bare `{}`)
  // silently decoded to allow regardless of exit code -- this is what
  // decodeKnownShape's "recognized" flag now prevents, mirroring the
  // claude/codex codecs. Gemini's wire never reveals which event a
  // decision answers, so decision.event is always left unset
  // (invariant 6).
  decodeDecision(stdout: unknown, exit: number): Decision {
    if (isEmptyStdout(stdout)) {
      return { action: actionForExit(this.caps, exit) };
    }
    const m = asRecord(stdout, "gemini decision");
    const { decision, recognized } = decodeKnownShape(m);
    if (!recognized) {
      return { action: actionForExit(this.caps, exit) };
    }
    return decision;
  }

  // checkEvent rejects a decision naming an event Gemini has no wire
  // shape for. A blank event keeps the default shape.
  private checkEvent(ev: Event | undefined): void {
    if (ev === undefined || ev === "") return;
    const { level: lvl, ok } = level(this.caps, ev);
    if (!ok || (lvl !== "native" && lvl !== "close")) {
      throw new UnsupportedEventError("gemini", ev);
    }
  }
}

function requireNativeOrClose(caps: Capabilities, host: string, event: Event): void {
  const { level: lvl, ok } = level(caps, event);
  if (!ok || (lvl !== "native" && lvl !== "close")) {
    throw new UnsupportedEventError(host, event);
  }
}

// decodeKnownShape probes the wire object for Gemini's known decision
// shapes and reports whether any were found. The systemMessage-based
// warn/allow fold is preserved exactly as before this gate was added: it
// disambiguates two shapes already recognized via "decision" rather than
// participating in recognition itself, mirroring hooks/hosts/gemini/codec.go.
function decodeKnownShape(m: Record<string, unknown>): { decision: Decision; recognized: boolean } {
  const d: Decision = { action: "allow" };
  let recognized = false;

  if (m.decision === "deny") {
    recognized = true;
    d.action = "block";
  } else if (m.decision === "allow") {
    recognized = true;
    d.action = "allow";
  }
  d.message = typeof m.reason === "string" ? m.reason : "";
  // The wire is lossy: plain allow and warn both carry decision:"allow"
  // plus "reason". Only "systemMessage" (set exclusively by the Warn
  // branch) distinguishes them; its absence means this genuinely cannot
  // be told apart from allow.
  if (typeof m.systemMessage === "string" && m.systemMessage !== "") {
    d.action = "warn";
  }
  const hso = isRecord(m.hookSpecificOutput) ? m.hookSpecificOutput : undefined;
  if (hso && "tool_input" in hso) {
    recognized = true;
    d.action = "rewrite";
    d.rewrite = hso.tool_input;
  }
  return { decision: d, recognized };
}

// decodeMap: native rows always win over close rows. Gemini's own
// motivating case: host event SessionEnd is listed native -> SessionEnd
// and close -> Stop; only the native row is what Gemini actually emits.
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

function actionForExit(caps: Capabilities, exit: number): Decision["action"] {
  const codes = hostExitCodes(caps);
  if (exit === codes.block && exit !== codes.allow) return "block";
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
  return new GeminiCodec();
}
