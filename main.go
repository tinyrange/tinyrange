package main

import (
	"embed"

	cli "github.com/tinyrange/tinyrange/cmd/tinyrange"
	"github.com/tinyrange/tinyrange/pkg/common"
)

//go:embed cmd/* pkg/* go.mod go.sum tools/* LICENSE main.go stdlib/* third_party/*
var SOURCE_FS embed.FS

func main() {
	common.SetSourceFS(SOURCE_FS)
	cli.Run()
}
