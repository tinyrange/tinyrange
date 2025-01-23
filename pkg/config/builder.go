package config

type BuilderConfig struct {
	HostAddress        string
	Commands           []string
	ServiceCommands    []string
	InitScripts        []string
	StarlarkScripts    []string
	Environment        []string
	ExecInit           string
	OutputFilename     string
	DefaultInteractive []string
}
