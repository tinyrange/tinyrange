package log

import (
	"bytes"
	"io"
	"log/slog"
	"os"
)

type Handler interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

var level slog.LevelVar

func SetVerbose() {
	level.Set(slog.LevelDebug)
}

type newlineReplaceWriter struct {
	w           io.Writer
	replaceWith string
}

func (w *newlineReplaceWriter) Write(p []byte) (n int, err error) {
	return w.w.Write(bytes.ReplaceAll(p, []byte("\n"), []byte(w.replaceWith)))
}

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(&newlineReplaceWriter{
		w:           os.Stderr,
		replaceWith: "\r\n",
	}, &slog.HandlerOptions{
		Level: &level,
	})))
}

type defaultHandler struct{}

// Debug implements Handler.
func (d *defaultHandler) Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

// Error implements Handler.
func (d *defaultHandler) Error(msg string, args ...any) {
	slog.Error(msg, args...)
}

// Info implements Handler.
func (d *defaultHandler) Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

// Warn implements Handler.
func (d *defaultHandler) Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

var (
	_ Handler = &defaultHandler{}
)

var (
	_defaultHandler = &defaultHandler{}
)

func Default() Handler {
	return _defaultHandler
}
