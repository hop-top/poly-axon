package ident

import "testing"

func TestExport(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"PreToolUse", "PreToolUse"},
		{"cursor-agent", "CursorAgent"},
		{"tool.execute.before", "ToolExecuteBefore"},
		// No catalog event or host name uses '_' today; the separator is
		// still split on, so keep it covered.
		{"pre_tool_use", "PreToolUse"},
	}
	for _, c := range cases {
		if got := Export(c.name); got != c.want {
			t.Errorf("Export(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
