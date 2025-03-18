package log

import (
	"bytes"
	"io"
	"log/slog"
	"os"
)

var level slog.LevelVar

var Debug = slog.Debug
var Info = slog.Info
var Warn = slog.Warn
var Error = slog.Error

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
