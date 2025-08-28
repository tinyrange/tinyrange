package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tinyrange/tinyrange/experimental/fs3/structgen/libstruct"
	"github.com/tinyrange/tinyrange/pkg/log"
)

var (
	inputFilename  = flag.String("input", "", "the .struct file to parse and generate code for")
	outputFilename = flag.String("output", "", "the output file to write the generated code to (if empty print to stdout)")
	packageName    = flag.String("package", "main", "the package name to use for the generated code")
)

func appMain() error {
	flag.Parse()

	if *inputFilename == "" {
		return fmt.Errorf("input filename is required")
	}

	file, err := os.Open(*inputFilename)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer file.Close()

	ast, err := libstruct.Parse(file)
	if err != nil {
		return err
	}

	generator := libstruct.NewSysIL4Generator(*packageName)

	if err := generator.Generate(ast); err != nil {
		return err
	}

	if *outputFilename == "" {
		if _, err := generator.WriteTo(os.Stdout); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
	} else {
		outFile, err := os.Create(*outputFilename)
		if err != nil {
			return fmt.Errorf("failed to open output file: %w", err)
		}
		defer outFile.Close()

		if _, err := generator.WriteTo(outFile); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Default().Error("fatal", "error", err)
		os.Exit(1)
	}
}
