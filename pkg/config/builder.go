package config

type BuilderConfig struct {
	HostAddress        string
	Commands           []string
	ServiceCommands    []string
	InitScripts        []string
	Environment        []string
	ExecInit           string
	OutputFilename     string
	DefaultInteractive []string
}
