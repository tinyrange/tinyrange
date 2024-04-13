package core

import "go.starlark.net/starlark"

type Logger interface {
	Log(message string)
}

type nilLogger struct{}

// Log implements Logger.
func (n *nilLogger) Log(message string) {

}

var (
	_ Logger = &nilLogger{}
)

func GetLogger(thread *starlark.Thread) Logger {
	local := thread.Local("logger")
	if local == nil {
		return &nilLogger{}
	}
	logger, ok := local.(Logger)
	if !ok {
		return &nilLogger{}
	}
	return logger
}

func SetLogger(thread *starlark.Thread, log Logger) {
	thread.SetLocal("logger", log)
}

func Log(logger Logger, message string) {
	if logger != nil {
		logger.Log(message)
	}
}
