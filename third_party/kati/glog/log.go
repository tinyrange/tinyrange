package glog

import (
	"fmt"
	"log/slog"
)

const (
	ENABLE_LOGGING = true
	LOGGING_LEVEL  = 0
)

type Logger bool

func (l Logger) Info(val ...any) {
	if !l {
		return
	}

	Info(val...)
}

func (l Logger) Infof(fmt string, args ...any) {
	if !l {
		return
	}

	Infof(fmt, args...)
}

func Info(val ...any) {
	slog.Info("", "values", val)
}

func Infof(f string, args ...any) {
	slog.Info(fmt.Sprintf(f, args...))
}

func Warning(val ...any) {
	slog.Warn("", "values", val)
}

func Warningf(f string, args ...any) {
	slog.Warn(fmt.Sprintf(f, args...))
}

func Errorf(f string, args ...any) {
	slog.Error(fmt.Sprintf(f, args...))
}

func V(verbose int) Logger {
	if verbose > LOGGING_LEVEL {
		return Logger(false)
	} else {
		return Logger(true)
	}
}
