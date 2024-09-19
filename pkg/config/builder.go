package config

type BuilderConfig struct {
	HostAddress        string
	Commands           []string
	Environment        []string
	ExecInit           string
	OutputFilename     string
	DefaultInteractive []string
}
