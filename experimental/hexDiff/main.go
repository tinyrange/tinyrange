package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/fatih/color"
	"github.com/tinyrange/tinyrange/pkg/log"
)

var (
	width = flag.Int("width", 32, "the width of the hex output")
	all   = flag.Bool("all", false, "ignored changed values and print everything")
	stop  = flag.String("stop", "", "stop after x bytes have been read")
)

func printable(i byte) string {
	if i >= 'a' && i <= 'z' {
		return string(i)
	}
	if i >= 'A' && i <= 'Z' {
		return string(i)
	}
	if i >= '0' && i <= '9' {
		return string(i)
	}
	if i == '.' {
		return "."
	}
	if i == 0x00 {
		return " "
	}
	if i == 0xff {
		return "%"
	}

	return "-"
}

func printDiff(off int64, aA []byte, bA []byte, all bool) string {
	if len(aA) != len(bA) {
		panic("different sizes passed to printDiff")
	}

	changed := false

	if all {
		changed = true
	} else {
		for i := 0; i < len(aA); i++ {
			a := aA[i]
			b := bA[i]

			if a != b {
				changed = true
				break
			}
		}

		if !changed {
			return ""
		}
	}

	white := color.New(color.FgWhite).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	outLine1 := fmt.Sprintf("B: %08X: ", off)
	outLine2 := fmt.Sprintf("A: %08X: ", off)

	outLine1Print := " : "
	outLine2Print := " : "

	for i := 0; i < len(aA); i++ {
		a := aA[i]
		b := bA[i]

		if a != b {
			outLine1 += green(fmt.Sprintf("%02x", b))
			outLine2 += green(fmt.Sprintf("%02x", a))
			outLine1Print += green(printable(b))
			outLine2Print += green(printable(a))
		} else {
			outLine1 += white("--")
			outLine2 += white(fmt.Sprintf("%02x", a))
			outLine1Print += white("-")
			outLine2Print += white(printable(a))
		}

		if i%4 == 3 {
			outLine1 += " "
			outLine2 += " "
		}
	}

	if changed {
		return outLine1 + outLine1Print + "\n" + outLine2 + outLine2Print + "\n"
	} else {
		return ""
	}
}

func appMain() error {
	flag.Parse()

	args := flag.Args()

	if len(args) != 2 {
		flag.Usage()
		return nil
	}

	a, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer a.Close()

	b, err := os.Open(args[1])
	if err != nil {
		return err
	}
	defer b.Close()

	var stopInt int64 = -1
	if *stop != "" {
		stopI, err := strconv.ParseInt(*stop, 0, 64)
		if err != nil {
			return err
		}

		stopInt = stopI
	}

	aReader := bufio.NewReader(a)
	bReader := bufio.NewReader(b)

	aBuf := make([]byte, *width)
	bBuf := make([]byte, *width)

	var off int64 = 0

	for {
		var err error

		_, err = aReader.Read(aBuf)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		_, err = bReader.Read(bBuf)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		out := printDiff(off, aBuf, bBuf, *all)

		if out != "" {
			fmt.Print(out)
		}

		off += int64(*width)

		if stopInt != -1 && off >= stopInt {
			break
		}
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}
