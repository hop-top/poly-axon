"""Importing this module registers the codecs for every hooked host that
currently has one (claude, codex, gemini, opencode -- contract notes S4).
Mirrors hop.top/axon/hooks/hosts's blank-import side effect and
ts/src/hooks/hosts/all.ts.
"""

from __future__ import annotations

from axon.hooks.hosts.claude import make_codec as _make_claude_codec
from axon.hooks.hosts.codex import make_codec as _make_codex_codec
from axon.hooks.hosts.gemini import make_codec as _make_gemini_codec
from axon.hooks.hosts.opencode import make_codec as _make_opencode_codec
from axon.hooks.registry import register

register(_make_claude_codec())
register(_make_codex_codec())
register(_make_gemini_codec())
register(_make_opencode_codec())
