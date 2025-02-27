package main

import (
	"flag"
	"log"
	"os"
	"os/exec"

	"github.com/tinyrange/tinyrange/cmd/structgen/sysil2/codegen"
	"github.com/tinyrange/tinyrange/cmd/structgen/sysil2/parser"
)

var (
	input       = flag.String("input", "", "the .sysil file to generate code for")
	output      = flag.String("output", "", "the .go file to write based on the sysil file")
	packageName = flag.String("package", "main", "the name of the package to write")
)

func formatFile(filename string) error {
	cmd := exec.Command("go", "fmt", filename)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func writeFile(filename string, gen *codegen.StructureCodeGenerator) error {
	out, err := os.Create(*output)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = gen.WriteTo(out)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	flag.Parse()

	if *input == "" || *output == "" {
		flag.Usage()

		return
	}

	in, err := os.Open(*input)
	if err != nil {
		log.Fatal(err)
	}
	defer in.Close()

	file, err := parser.Parse(in)
	if err != nil {
		log.Fatal(err)
	}

	gen := codegen.NewStructureCodeGenerator(*packageName)

	if err := gen.AddFile(file); err != nil {
		log.Fatal(err)
	}

	if err := writeFile(*output, gen); err != nil {
		log.Fatal(err)
	}

	if err := formatFile(*output); err != nil {
		log.Fatal(err)
	}
}
