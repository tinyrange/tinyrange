package config

type BuilderConfig struct {
	HostAddress    string
	Commands       []string
	Environment    []string
	OutputFilename string
}
