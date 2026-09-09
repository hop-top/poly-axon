package axon

// StorePaths holds XDG-style root paths for a host's persistent stores.
type StorePaths struct {
	Config string `yaml:"config,omitempty"`
	Data   string `yaml:"data,omitempty"`
	Cache  string `yaml:"cache,omitempty"`
	State  string `yaml:"state,omitempty"`
}

// ExitCodes maps a host's hook decision actions to process exit codes.
// Zero value when the host has no exit-code contract; codecs then carry
// decisions in stdout only.
type ExitCodes struct {
	Allow   int `yaml:"allow"`
	Warn    int `yaml:"warn,omitempty"`
	Block   int `yaml:"block"`
	Rewrite int `yaml:"rewrite,omitempty"`
	Error   int `yaml:"error,omitempty"`
}

// Host describes one axon-identified coding-assistant CLI.
type Host struct {
	Name                  string     `yaml:"name"`
	Status                string     `yaml:"status"`
	Aliases               []string   `yaml:"aliases,omitempty"`
	Binaries              []string   `yaml:"binaries"`
	StoreRoots            StorePaths `yaml:"store_roots,omitempty"`
	ConfigFilePatterns    []string   `yaml:"config_file_patterns,omitempty"`
	ProjectKeyStrategy    string     `yaml:"project_key_strategy,omitempty"`
	HookConfigPaths       []string   `yaml:"hook_config_paths,omitempty"`
	ExitCodes             ExitCodes  `yaml:"exit_codes,omitempty"`
	EnvelopeDiscriminator string     `yaml:"envelope_discriminator,omitempty"`
	Hooks                 bool       `yaml:"hooks,omitempty"`
}
