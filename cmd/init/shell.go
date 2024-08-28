//go:build linux

package main

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anmitsu/go-shlex"
	"github.com/wader/readline"
)

type shellInstance struct {
	rl *readline.Instance
}

var (
	ErrExit = fmt.Errorf("exit")
)

func (c *shellInstance) getLocalFile(common string) func(s string) []string {
	return func(s string) []string {
		initialPath := strings.TrimPrefix(s, common)
		if initialPath == "" {
			initialPath = "."
		}

		dirName := filepath.Dir(initialPath)

		// TODO(joshua): complete host files
		files, err := os.ReadDir(dirName)
		if err != nil {
			return []string{}
		}

		var ret []string
		for _, file := range files {
			if file.IsDir() {
				ret = append(ret, filepath.Join(dirName, file.Name())+"/")
			} else {
				ret = append(ret, filepath.Join(dirName, file.Name()))
			}
		}
		return ret
	}
}

func (c *shellInstance) getCompleter() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("exit"),
		readline.PcItem("ls", readline.PcItemDynamic(c.getLocalFile("ls "))),
		readline.PcItem("cd", readline.PcItemDynamic(c.getLocalFile("cd "))),
		readline.PcItem("mkdir", readline.PcItemDynamic(c.getLocalFile("mkdir "))),
		readline.PcItem("cat", readline.PcItemDynamic(c.getLocalFile("cat "))),
		readline.PcItem("chmod", readline.PcItem("+x"), readline.PcItemDynamic(c.getLocalFile("cat +x "))),
	)
}

func (sh *shellInstance) processLine(line string) error {
	defer func() {
		err := recover()

		if err != nil {
			slog.Error("caught panic", "err", err)
		}
	}()

	line = strings.Trim(line, " ")

	tokens, err := shlex.Split(line, true)
	if err != nil {
		return err
	}

	switch true {
	case tokens[0] == "":
		return nil
	case strings.HasPrefix(line, "exit"):
		return ErrExit
	case strings.HasPrefix(line, "ls"):
		directory := "."
		if len(tokens) > 1 {
			directory = tokens[1]
		}

		ents, err := os.ReadDir(directory)
		if err != nil {
			return err
		}

		for _, ent := range ents {
			slog.Info("", "ent", ent)
		}

		return nil
	case strings.HasPrefix(line, "cd"):
		directory := "."
		if len(tokens) > 1 {
			directory = tokens[1]
		}

		if err := os.Chdir(directory); err != nil {
			return err
		}

		return nil
	case strings.HasPrefix(line, "mkdir"):
		directory := "."
		if len(tokens) > 1 {
			directory = tokens[1]
		}

		if err := os.Mkdir(directory, os.ModePerm); err != nil {
			return err
		}

		return nil
	case strings.HasPrefix(line, "cat"):
		filename := "."
		if len(tokens) > 1 {
			filename = tokens[1]
		}

		f, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(os.Stdout, f); err != nil {
			return err
		}

		return nil
	case strings.HasPrefix(line, "chmod"):
		if tokens[1] != "+x" {
			return fmt.Errorf("only +x is implemented")
		}
		filename := "."
		if len(tokens) > 2 {
			filename = tokens[2]
		}

		info, err := os.Stat(filename)
		if err != nil {
			return err
		}
		mode := info.Mode() | fs.FileMode(0111)

		if err := os.Chmod(filename, mode); err != nil {
			return err
		}

		return nil
	case strings.HasPrefix(line, "env"):
		for _, env := range os.Environ() {
			slog.Info("", "env", env)
		}

		return nil
	default:
		cmd := exec.Command(tokens[0], tokens[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			return err
		}

		return nil
	}
}

func (sh *shellInstance) updatePrompt() {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "<unknown wd>"
	}

	sh.rl.SetPrompt(cwd + " \033[94m# \033[0m")
}

func shellMain() error {
	var err error

	sh := &shellInstance{}

	sh.rl, err = readline.NewEx(&readline.Config{
		Prompt:       "\033[94m>>> \033[0m",
		AutoComplete: sh.getCompleter(),
	})
	if err != nil {
		return err
	}

	sh.updatePrompt()

	for {
		line, err := sh.rl.Readline()
		if err == readline.ErrInterrupt {
			continue
		} else if err != nil {
			return err
		}

		err = sh.processLine(line)
		if err == ErrExit {
			break
		} else if err != nil {
			slog.Info("", "error", err)
		}

		sh.updatePrompt()

		sh.rl.Refresh()
	}

	return nil
}
