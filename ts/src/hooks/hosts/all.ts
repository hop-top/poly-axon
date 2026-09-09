// Importing this module registers the codecs for every hooked host that
// currently has one (claude, codex, gemini, opencode -- contract notes
// §4). Mirrors hop.top/axon/hooks/hosts's blank-import side effect.
import { register } from "../registry";
import { makeCodec as makeClaudeCodec } from "./claude";
import { makeCodec as makeCodexCodec } from "./codex";
import { makeCodec as makeGeminiCodec } from "./gemini";
import { makeCodec as makeOpencodeCodec } from "./opencode";

register(makeClaudeCodec());
register(makeCodexCodec());
register(makeGeminiCodec());
register(makeOpencodeCodec());
