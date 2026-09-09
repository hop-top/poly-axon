package axon

import "testing"

func TestDeriveKey(t *testing.T) {
	const cwd = "/Users/jadb/.w/ideacrafterslabs/uhp"

	tests := []struct {
		name     string
		strategy string
		want     string
	}{
		{
			name:     "SlashToDash",
			strategy: "slash-to-dash",
			want:     "Users-jadb-.w-ideacrafterslabs-uhp",
		},
		{
			name:     "SHA1",
			strategy: "sha1",
			want:     "dc46c5c89af35341f834cfb93af103343ac31158",
		},
		{
			name:     "SHA256",
			strategy: "sha256",
			want:     "b2e9434903c3d2059d2c5c0d60467de55a64165e331ac29fb235ee4a0d7b64b9",
		},
		{
			name:     "MD5",
			strategy: "md5",
			want:     "04c0ec06e2a2388290e4bffa13f97131",
		},
		{
			name:     "BasenameAlias",
			strategy: "basename-alias",
			want:     "uhp",
		},
		{
			name:     "Embedded",
			strategy: "embedded",
			want:     cwd,
		},
		{
			name:     "None",
			strategy: "none",
			want:     cwd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DeriveKey(cwd, tt.strategy)
			if err != nil {
				t.Fatalf("DeriveKey(%q, %s): %v", cwd, tt.strategy, err)
			}
			if got != tt.want {
				t.Errorf("DeriveKey(%q, %s) = %q, want %q", cwd, tt.strategy, got, tt.want)
			}
		})
	}
}

// Regression: SlashToDash must not panic on empty or root-only input.
func TestDeriveKey_SlashToDashEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
		want string
	}{
		{"empty string", "", ""},
		{"root only", "/", ""},
		{"single segment", "foo", "foo"},
		{"trailing slash", "/Users/jadb/", "Users-jadb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("DeriveKey(%q, slash-to-dash) panicked: %v", tt.cwd, r)
				}
			}()
			got, err := DeriveKey(tt.cwd, "slash-to-dash")
			if err != nil {
				t.Fatalf("DeriveKey(%q, slash-to-dash): %v", tt.cwd, err)
			}
			if got != tt.want {
				t.Errorf("DeriveKey(%q, slash-to-dash) = %q, want %q", tt.cwd, got, tt.want)
			}
		})
	}
}

// DeriveKey must reject any strategy outside the host schema's
// project_key_strategy enum rather than silently falling back to a
// default derivation.
func TestDeriveKey_UnknownStrategy(t *testing.T) {
	_, err := DeriveKey("/some/path", "bogus-strategy")
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

func TestProjectKeyStrategies_MatchSchemaEnum(t *testing.T) {
	want := []string{
		"slash-to-dash", "sha1", "sha256", "md5",
		"basename-alias", "embedded", "none",
	}
	if len(projectKeyStrategies) != len(want) {
		t.Fatalf("projectKeyStrategies has %d entries, want %d", len(projectKeyStrategies), len(want))
	}
	for _, s := range want {
		if _, ok := projectKeyStrategies[s]; !ok {
			t.Errorf("projectKeyStrategies missing %q", s)
		}
	}
}
