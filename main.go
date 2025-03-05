package main

import (
	"github.com/tinyrange/tinyrange/pkg/cli"
	"github.com/tinyrange/tinyrange/pkg/linux/goboot"
)

func main() {
	if goboot.MaybeExecInit() {
		return
	}

	cli.Run()
}
