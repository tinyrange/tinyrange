package jinja2

import (
	"fmt"
	"math/rand"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type loopObject struct {
	length int
	i      int
}

// Attr implements starlark.HasAttrs.
func (l *loopObject) Attr(name string) (starlark.Value, error) {
	if name == "last" {
		if l.i == l.length-1 {
			return starlark.True, nil
		} else {
			return starlark.False, nil
		}
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (l *loopObject) AttrNames() []string {
	return []string{}
}

func (*loopObject) String() string { return "loopObject" }
func (*loopObject) Type() string   { return "loopObject" }
func (*loopObject) Hash() (uint32, error) {
	return 0, fmt.Errorf("loopObject is not hashable")
}
func (*loopObject) Truth() starlark.Bool { return starlark.True }
func (*loopObject) Freeze()              {}

var (
	_ starlark.Value    = &loopObject{}
	_ starlark.HasAttrs = &loopObject{}
)

type starlarkEmitter struct {
	out    string
	indent int
}

func (f *starlarkEmitter) emit(s string) {
	for i := 0; i < f.indent; i++ {
		f.out += "  "
	}
	f.out += s
	f.out += "\n"
}

func (f *starlarkEmitter) emitText(s string) {
	f.emit("emit(" + starlark.String(s).String() + ")")
}

func (f *starlarkEmitter) emitExpression(s string) error {
	var err error

	s = strings.Trim(s, " ")

	if strings.HasPrefix(s, "raise(") {
		s = "_" + s
	}

	if strings.Contains(s, "|") {
		expr := s

		tokens := strings.Split(expr, "|")
		expr = tokens[0]

		for _, token := range tokens[1:] {
			expr, err = f.emitFilter(expr, token)
			if err != nil {
				return err
			}
		}

		f.emit("emit(" + expr + ")")
	} else {
		f.emit("emit(" + s + ")")
	}

	return nil
}

func (f *starlarkEmitter) emitFilter(ret string, filter string) (string, error) {
	filter = strings.Trim(filter, " ")

	switch true {
	case filter == "map('int')":
		return fmt.Sprintf("map(%s, int)", ret), nil
	case strings.HasPrefix(filter, "default("):
		value := strings.TrimPrefix(filter, "default(")
		value = strings.TrimSuffix(value, ")")
		return fmt.Sprintf("_def(%s, %s)", ret, value), nil
	case strings.HasPrefix(filter, "list "):
		return fmt.Sprintf("%s %s", ret, strings.TrimPrefix(filter, "list ")), nil
	default:
		return "", fmt.Errorf("filter not implemented: %s", filter)
	}
}

func (f *starlarkEmitter) emitStatement(s string) error {
	var err error

	trimStart := strings.HasPrefix(s, "-")
	trimEnd := strings.HasSuffix(s, "-")

	_ = trimStart
	_ = trimEnd

	s = strings.Trim(s, "- ")

	if strings.HasPrefix(s, "if ") {
		expr := strings.TrimPrefix(s, "if ")

		if strings.Contains(expr, "|") {
			tokens := strings.Split(expr, "|")
			expr = tokens[0]

			for _, token := range tokens[1:] {
				expr, err = f.emitFilter(expr, token)
				if err != nil {
					return err
				}
			}
		}

		f.emit(fmt.Sprintf("if %s:", expr))
		f.indent += 1

		return nil
	} else if strings.HasPrefix(s, "for ") {
		bind, lst, ok := strings.Cut(strings.TrimPrefix(s, "for "), " in ")
		if !ok {
			return fmt.Errorf("unknown for format: %s", s)
		}
		id := rand.Intn(0xffff)
		lstName := fmt.Sprintf("_%d_lst", id)
		bindName := fmt.Sprintf("_%d_i", id)
		f.emit(fmt.Sprintf("%s = %s", lstName, lst))
		f.emit(fmt.Sprintf("for %s, %s in enumerate(%s):", bindName, bind, lstName))
		f.indent += 1
		f.emit(fmt.Sprintf("loop = _mkloop(len(%s), %s)", lstName, bindName))

		return nil
	} else if strings.HasPrefix(s, "elif ") {
		f.indent -= 1
		f.emit(s + ":")
		f.indent += 1

		return nil
	} else if strings.HasPrefix(s, "set ") {
		f.emit(strings.TrimPrefix(s, "set "))

		return nil
	} else if s == "else" {
		f.indent -= 1
		f.emit(s + ":")
		f.indent += 1

		return nil
	} else if s == "endif" {
		f.indent -= 1

		return nil
	} else if s == "endfor" {
		f.indent -= 1

		return nil
	} else {
		return fmt.Errorf("unknown statement: %s", s)
	}
}

type Jinja2Evaluator struct {
	environment starlark.StringDict
	output      string
}

// Attr implements starlark.HasAttrs.
func (j *Jinja2Evaluator) Attr(name string) (starlark.Value, error) {
	if val, ok := j.environment[name]; ok {
		return val, nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (j *Jinja2Evaluator) AttrNames() []string {
	var ret []string

	for k := range j.environment {
		ret = append(ret, k)
	}

	return ret
}

func (j *Jinja2Evaluator) ToStarlark(contents string) (string, error) {
	emitter := &starlarkEmitter{}
	for {
		start := strings.IndexByte(contents, '{')
		if start == -1 {
			emitter.emitText(contents)
			break
		}
		next := contents[start+1]
		switch next {
		case '{':
			end := strings.Index(contents, "}}")
			if end == -1 {
				return "", fmt.Errorf("invalid syntax: could not find expression end")
			}

			emitter.emitText(contents[:start])

			expr := contents[start+2 : end]

			if err := emitter.emitExpression(expr); err != nil {
				return "", err
			}

			contents = contents[end+2:]
		case '%':
			end := strings.Index(contents, "%}")
			if end == -1 {
				return "", fmt.Errorf("invalid syntax: could not find statement end")
			}

			emitter.emitText(contents[:start])

			stmt := contents[start+2 : end]

			if err := emitter.emitStatement(stmt); err != nil {
				return "", err
			}

			contents = contents[end+2:]
		default:
			emitter.emitText(contents[:start+1])

			contents = contents[start+2:]
		}
	}

	return emitter.out, nil
}

func (j *Jinja2Evaluator) emitString(
	thread *starlark.Thread,
	fn *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var (
		str string
	)

	if err := starlark.UnpackArgs("Jinja2Evaluator.emit", args, kwargs,
		"str", &str,
	); err != nil {
		return starlark.None, err
	}

	j.output += str

	return starlark.None, nil
}

func (j *Jinja2Evaluator) Eval(contents string, environment []starlark.Tuple) (string, error) {
	script, err := j.ToStarlark(contents)
	if err != nil {
		return "", err
	}

	fmt.Printf("%s\n", script)

	topLevel := ""

	j.environment = make(starlark.StringDict)
	for _, tup := range environment {
		k := tup[0].(starlark.String)
		v := tup[1]

		if string(k) == "__top_level" {
			topLevel = string(v.(starlark.String))
		}

		j.environment[string(k)] = v
	}

	thread := &starlark.Thread{}

	decl := starlark.StringDict{
		"map": starlark.NewBuiltin("map", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				lst *starlark.List
				f   starlark.Value
			)

			if err := starlark.UnpackArgs("map", args, kwargs,
				"lst", &lst,
				"f", &f,
			); err != nil {
				return starlark.None, err
			}

			var elems []starlark.Value

			var mapError error = nil

			lst.Elements(func(v starlark.Value) bool {
				val, err := starlark.Call(thread, f, starlark.Tuple{v}, []starlark.Tuple{})
				if err != nil {
					mapError = err
					return false
				}
				elems = append(elems, val)
				return true
			})
			if mapError != nil {
				return starlark.None, mapError
			}

			return starlark.NewList(elems), nil
		}),
		"_mkloop": starlark.NewBuiltin("_mkloop", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				length int
				i      int
			)

			if err := starlark.UnpackArgs("map", args, kwargs,
				"length", &length,
				"i", &i,
			); err != nil {
				return starlark.None, err
			}

			return &loopObject{length: length, i: i}, nil
		}),
		"_raise": starlark.NewBuiltin("_raise", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				message string
			)

			if err := starlark.UnpackArgs("_raise", args, kwargs,
				"message", &message,
			); err != nil {
				return starlark.None, err
			}

			return starlark.None, fmt.Errorf("%s", message)
		}),
		"_def": starlark.NewBuiltin("_def", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				a starlark.Value
				b starlark.Value
			)

			if err := starlark.UnpackArgs("_def", args, kwargs,
				"a", &a,
				"b", &b,
			); err != nil {
				return starlark.None, err
			}

			if a == starlark.None {
				return b, nil
			} else {
				return a, nil
			}
		}),
		"emit":   starlark.NewBuiltin("emit", j.emitString),
		"self":   j,
		topLevel: j,
	}

	_, err = starlark.ExecFileOptions(&syntax.FileOptions{
		TopLevelControl: true,
		GlobalReassign:  true,
	}, thread, "<input>", script, decl)
	if err != nil {
		return "", err
	}

	return j.output, nil
}

func (*Jinja2Evaluator) String() string { return "Jinja2Evaluator" }
func (*Jinja2Evaluator) Type() string   { return "Jinja2Evaluator" }
func (*Jinja2Evaluator) Hash() (uint32, error) {
	return 0, fmt.Errorf("Jinja2Evaluator is not hashable")
}
func (*Jinja2Evaluator) Truth() starlark.Bool { return starlark.True }
func (*Jinja2Evaluator) Freeze()              {}

var (
	_ starlark.Value    = &Jinja2Evaluator{}
	_ starlark.HasAttrs = &Jinja2Evaluator{}
)
