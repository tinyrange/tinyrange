package db

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	xj "github.com/basgys/goxml2json"
	"github.com/icza/dyno"
	"github.com/tinyrange/pkg2/pkg/cmake"
	"github.com/tinyrange/pkg2/pkg/common"
	"github.com/tinyrange/pkg2/pkg/core"
	"github.com/tinyrange/pkg2/pkg/jinja2"
	"github.com/tinyrange/pkg2/third_party/regexp"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"gopkg.in/yaml.v2"
	"howett.net/plist"
)

func (db *PackageDatabase) getGlobals(name string) (starlark.StringDict, error) {
	globals := starlark.StringDict{
		"add_mirror": starlark.NewBuiltin("add_mirror", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name       string
				mirrorList starlark.Iterable
			)

			if err := starlark.UnpackArgs("add_mirror", args, kwargs,
				"name", &name,
				"mirror_list", &mirrorList,
			); err != nil {
				return starlark.None, err
			}

			var mirrors []string

			it := mirrorList.Iterate()
			defer it.Done()

			var val starlark.Value
			for it.Next(&val) {
				str, ok := starlark.AsString(val)
				if !ok {
					return starlark.None, fmt.Errorf("could not convert %s to string", val.Type())
				}

				mirrors = append(mirrors, str)
			}

			if err := db.Eif.AddMirror(name, mirrors); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}),
		"fetch_http": starlark.NewBuiltin("fetch_http", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				url          string
				expectedSize int64
				accept       string
				useETag      bool
				fast         bool
				expireTime   int64
				params       *starlark.Dict
				waitTime     int64
			)

			if err := starlark.UnpackArgs("fetch_http", args, kwargs,
				"url", &url,
				"expected_size?", &expectedSize,
				"accept?", &accept,
				"use_etag?", &useETag,
				"fast?", &fast,
				"expire_time?", &expireTime,
				"params?", &params,
				"wait_time?", &waitTime,
			); err != nil {
				return starlark.None, err
			}

			var paramsMap map[string]string

			if params != nil {
				paramsMap = make(map[string]string)

				var err error
				params.Entries(func(k, v starlark.Value) bool {
					kStr, ok := starlark.AsString(k)
					if !ok {
						err = fmt.Errorf("could not convert %s to String", k.Type())
						return false
					}

					vStr, ok := starlark.AsString(v)
					if !ok {
						err = fmt.Errorf("could not convert %s to String", v.Type())
						return false
					}

					paramsMap[kStr] = vStr

					return true
				})
				if err != nil {
					return starlark.None, err
				}
			}

			f, err := db.Eif.HttpGetReader(url, core.HttpOptions{
				ExpectedSize: expectedSize,
				Accept:       accept,
				UseETag:      useETag,
				FastDownload: fast,
				ExpireTime:   time.Duration(expireTime),
				Logger:       core.GetLogger(thread),
				Params:       paramsMap,
				WaitTime:     time.Duration(waitTime),
			})
			if err == core.ErrNotFound {
				return starlark.None, nil
			} else if err != nil {
				return starlark.None, err
			}
			defer f.Close()

			filename := f.Name()

			return NewFile(
				common.DownloadSource{
					Kind:   "Download",
					Url:    url,
					Accept: accept,
				},
				url,
				func() (io.ReadCloser, error) {
					return os.Open(filename)
				},
				nil,
			), nil
		}),
		"fetch_git": starlark.NewBuiltin("fetch_git", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				url string
			)

			if err := starlark.UnpackArgs("fetch_git", args, kwargs,
				"url", &url,
			); err != nil {
				return starlark.None, err
			}

			return db.fetchGit(url)
		}),
		"register_content_fetcher": starlark.NewBuiltin("register_content_fetcher", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name  string
				f     *starlark.Function
				fArgs starlark.Tuple
			)

			if err := starlark.UnpackArgs("register_content_fetcher", args, kwargs,
				"name", &name,
				"f", &f,
				"fArgs", &fArgs,
			); err != nil {
				return starlark.None, err
			}

			if err := db.addContentFetcher(name, f, fArgs); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}),
		"register_script_fetcher": starlark.NewBuiltin("register_script_fetcher", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name  string
				f     *starlark.Function
				fArgs starlark.Tuple
			)

			if err := starlark.UnpackArgs("register_script_fetcher", args, kwargs,
				"name", &name,
				"f", &f,
				"fArgs", &fArgs,
			); err != nil {
				return starlark.None, err
			}

			if err := db.addScriptFetcher(name, f, fArgs); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}),
		"register_search_provider": starlark.NewBuiltin("register_search_provider", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				distro string
				f      *starlark.Function
				fArgs  starlark.Tuple
			)

			if err := starlark.UnpackArgs("register_search_provider", args, kwargs,
				"distro", &distro,
				"f", &f,
				"fArgs", &fArgs,
			); err != nil {
				return starlark.None, err
			}

			if err := db.addSearchProvider(distro, f, fArgs); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}),
		"fetch_repo": starlark.NewBuiltin("fetch_repo", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				distro string
				f      *starlark.Function
				fArgs  starlark.Tuple
			)

			if err := starlark.UnpackArgs("fetch_repo", args, kwargs,
				"f", &f,
				"fArgs", &fArgs,
				"distro?", &distro,
			); err != nil {
				return starlark.None, err
			}

			if err := db.addRepositoryFetcher(distro, f, fArgs); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}),
		"shell_context": starlark.NewBuiltin("shell_context", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			return &ShellContext{
				environ:  starlark.NewDict(32),
				files:    starlark.NewDict(32),
				state:    starlark.NewDict(32),
				commands: make(map[string]*shellCommand),
			}, nil
		}),
		"parse_yaml": starlark.NewBuiltin("parse_yaml", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("parse_yaml", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			var body interface{}
			if err := yaml.Unmarshal([]byte(contents), &body); err != nil {
				return starlark.None, err
			}

			body = dyno.ConvertMapI2MapS(body)

			if b, err := json.Marshal(body); err != nil {
				return starlark.None, err
			} else {
				return starlark.Call(
					thread,
					starlarkjson.Module.Members["decode"],
					starlark.Tuple{starlark.String(b)},
					[]starlark.Tuple{},
				)
			}
		}),
		"parse_xml": starlark.NewBuiltin("parse_xml", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("parse_xml", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			json, err := xj.Convert(strings.NewReader(contents))
			if err != nil {
				return starlark.None, err
			}

			return starlark.Call(
				thread,
				starlarkjson.Module.Members["decode"],
				starlark.Tuple{starlark.String(json.String())},
				[]starlark.Tuple{},
			)
		}),
		"parse_nix_derivation": starlark.NewBuiltin("parse_nix_derivation", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("parse_nix_derivation", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			return parseNixDerivation(thread, contents)
		}),
		"parse_plist": starlark.NewBuiltin("parse_plist", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("parse_plist", args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			var obj any

			if _, err := plist.Unmarshal([]byte(contents), &obj); err != nil {
				return starlark.None, err
			}

			bytes, err := json.Marshal(obj)
			if err != nil {
				return starlark.None, err
			}

			return starlark.Call(
				thread,
				starlarkjson.Module.Members["decode"],
				starlark.Tuple{starlark.String(bytes)},
				[]starlark.Tuple{},
			)
		}),
		"parse_rpm": starlark.NewBuiltin("parse_rpm", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				file starlark.Value
			)

			if err := starlark.UnpackArgs("parse_rpm", args, kwargs,
				"file", &file,
			); err != nil {
				return starlark.None, err
			}

			if fileIf, ok := file.(common.FileIf); ok {
				return parseRpm(fileIf)
			} else {
				return starlark.None, fmt.Errorf("expected FileIf got %s", file.Type())
			}
		}),
		"eval_cmake": starlark.NewBuiltin("eval_cmake", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				file     starlark.Value
				ctx      *starlark.Dict
				commands *starlark.Dict
			)

			if err := starlark.UnpackArgs("eval_cmake", args, kwargs,
				"file", &file,
				"ctx", &ctx,
				"commands", &commands,
			); err != nil {
				return starlark.None, err
			}

			if fileIf, ok := file.(*StarDirectory); ok {
				return cmake.EvalCMake(fileIf, ctx, commands)
			} else {
				return starlark.None, fmt.Errorf("expected StarFileIf got %s", file.Type())
			}
		}),
		"eval_starlark": starlark.NewBuiltin("eval_starlark", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			contents, ok := starlark.AsString(args[0])
			if !ok {
				return starlark.None, fmt.Errorf("could not convert %s to string", args[0].Type())
			}

			return evalStarlark(contents, kwargs)
		}),
		"eval_python": starlark.NewBuiltin("eval_python", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			contents, ok := starlark.AsString(args[0])
			if !ok {
				return starlark.None, fmt.Errorf("could not convert %s to string", args[0].Type())
			}

			return evalPython(contents, kwargs)
		}),
		"eval_jinja2": starlark.NewBuiltin("eval_jinja2", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs("eval_jinja2", args, []starlark.Tuple{},
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			evaluator := &jinja2.Jinja2Evaluator{}

			out, err := evaluator.Eval(contents, kwargs)
			if err != nil {
				return starlark.None, err
			}

			return starlark.String(out), nil
		}),
		"open": starlark.NewBuiltin("open", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				filename string
			)

			if err := starlark.UnpackArgs("open", args, kwargs,
				"filename", &filename,
			); err != nil {
				return starlark.None, err
			}

			if !db.AllowLocal {
				return starlark.None, fmt.Errorf("open is only allowed if -allowLocal is passed")
			}

			if _, err := os.Stat(filename); err != nil {
				return starlark.None, err
			}

			return NewFile(
				common.LocalFileSource{
					Kind:     "LocalFile",
					Filename: filename,
				},
				filename,
				func() (io.ReadCloser, error) {
					return os.Open(filename)
				},
				nil,
			), nil
		}),
		"get_cache_filename": starlark.NewBuiltin("get_cache_filename", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				file *StarFile
			)

			if err := starlark.UnpackArgs("get_cache_filename", args, kwargs,
				"file", &file,
			); err != nil {
				return starlark.None, err
			}

			if !db.AllowLocal {
				return starlark.None, fmt.Errorf("get_cache_filename is only allowed if -allowLocal is passed")
			}

			f, err := file.opener()
			if err != nil {
				return starlark.None, err
			}
			defer f.Close()

			if file, ok := f.(*os.File); ok {
				return starlark.String(file.Name()), nil
			} else {
				return starlark.None, fmt.Errorf("could not get filename for %T", file)
			}
		}),
		"mutex": starlark.NewBuiltin("mutex", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			return &StarMutex{}, nil
		}),
		"duration": starlark.NewBuiltin("duration", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				hours        int64
				minutes      int64
				seconds      int64
				milliseconds int64
			)

			if err := starlark.UnpackArgs("duration", args, kwargs,
				"hours?", &hours,
				"minutes?", &minutes,
				"seconds?", &seconds,
				"milliseconds?", &milliseconds,
			); err != nil {
				return starlark.None, err
			}

			return starlark.MakeInt64(
				hours*int64(time.Hour) +
					minutes*int64(time.Minute) +
					seconds*int64(time.Second) +
					milliseconds*int64(time.Millisecond),
			), nil
		}),
		"builder": starlark.NewBuiltin("builder", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name PackageName
			)

			if err := starlark.UnpackArgs("builder", args, kwargs,
				"name", &name,
			); err != nil {
				return starlark.None, err
			}

			return NewBuilder(name), nil
		}),
		"filesystem": starlark.NewBuiltin("filesystem", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			fs := newFilesystem()

			switch len(args) {
			case 0:
				return fs, nil
			case 1:
				if ark, ok := args[0].(*StarArchive); ok {
					if err := fs.addArchive(ark); err == nil {
						return fs, nil
					} else {
						return starlark.None, err
					}
				} else {
					return starlark.None, fmt.Errorf("filesystem: expected StarArchive, got %s", args[0].Type())
				}
			default:
				return starlark.None, fmt.Errorf("filesystem: expected 0 or 1 arguments, got %d arguments", len(args))
			}
		}),
		"build_def": starlark.NewBuiltin("build_def", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				tag         starlark.Tuple
				builder     *starlark.Function
				builderArgs starlark.Tuple
			)

			if err := starlark.UnpackArgs("build_def", args, kwargs,
				"tag", &tag,
				"builder?", &builder,
				"builderArgs?", &builderArgs,
			); err != nil {
				return starlark.None, err
			}

			return newBuildDef(tag, builder, builderArgs)
		}),
		"file": starlark.NewBuiltin("file", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents   string
				executable bool
			)

			if err := starlark.UnpackArgs("file", args, kwargs,
				"contents", &contents,
				"executable?", &executable,
			); err != nil {
				return starlark.None, err
			}

			f := NewMemoryFile([]byte(contents))

			if executable {
				f.mode |= fs.FileMode(0111)
			}

			return f.WrapStarlark(""), nil
		}),
		"name": starlark.NewBuiltin("name", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				distribution string
				name         string
				version      string
				architecture string
			)

			if err := starlark.UnpackArgs("name", args, kwargs,
				"name", &name,
				"version?", &version,
				"distribution?", &distribution,
				"architecture?", &architecture,
			); err != nil {
				return starlark.None, err
			}

			return NewPackageName(distribution, name, version, architecture)
		}),
		"error": starlark.NewBuiltin("error", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				message string
			)

			if err := starlark.UnpackArgs("error", args, kwargs,
				"message", &message,
			); err != nil {
				return starlark.None, err
			}

			return starlark.None, fmt.Errorf("%s", message)
		}),
		"json":     starlarkjson.Module,
		"re":       regexp.Module,
		"__name__": starlark.String(name),
	}

	globals["run_script"] = starlark.NewBuiltin("run_script", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			f *starlark.Function
		)

		if err := starlark.UnpackArgs("run_script", args, kwargs,
			"f", &f,
		); err != nil {
			return starlark.None, err
		}

		db.scriptFunction = f

		return starlark.None, nil
	})

	return globals, nil
}
