package filesystem

import (
	"fmt"
	"io"
	"path"
)

type filesystemValidator struct {
	out io.Writer
}

func (v *filesystemValidator) validateFile(f File, p string) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}

	if info.IsDir() && info.Kind() == TypeRegular {
		return fmt.Errorf("%s: is a directory but has type regular", p)
	}

	switch info.Kind() {
	case TypeRegular:
		if info.Mode().Type() != 0 {
			return fmt.Errorf("%s: is a regular file but has mode type %s", p, info.Mode().Type())
		}
	case TypeDirectory:
	case TypeSymlink:
		target, err := GetLinkName(f)
		if err != nil {
			return err
		}

		if target == "" {
			return fmt.Errorf("%s: symlink has empty target", p)
		}
	case TypeLink:
		target, err := GetLinkName(f)
		if err != nil {
			return err
		}

		if target == "" {
			return fmt.Errorf("%s: link has empty target", p)
		}
	default:
		return fmt.Errorf("unknown kind: %s", info.Kind())
	}

	return nil
}

func (v *filesystemValidator) printFile(f File, p string, prefix string) error {
	if v.out == nil {
		return nil
	}

	info, err := f.Stat()
	if err != nil {
		return err
	}

	_ = info

	if _, err := fmt.Fprintf(v.out, "%s- %s % 8d %s\n", prefix, info.Mode(), info.Size(), path.Base(p)); err != nil {
		return err
	}

	return nil
}

func (v *filesystemValidator) validateAndDump(dir Directory, p string, prefix string) error {
	ents, err := dir.Readdir()
	if err != nil {
		return err
	}

	for _, ent := range ents {
		if path.Base(ent.Name) != ent.Name {
			return fmt.Errorf("file in %s has unclean name: %s != %s", p, path.Base(ent.Name), ent.Name)
		}

		if ent.Name == "" || ent.Name == "." {
			return fmt.Errorf("file in %s has empty or invalid name: %s", p, ent.Name)
		}

		name := path.Join(p, ent.Name)

		if err := v.validateFile(ent.File, name); err != nil {
			return err
		}

		if err := v.printFile(ent.File, name, prefix); err != nil {
			return err
		}

		if dir, ok := ent.File.(Directory); ok {
			if err := v.validateAndDump(dir, name, prefix+"  "); err != nil {
				return err
			}
		}
	}

	return nil
}

func ValidateAndDump(out io.Writer, top Directory) error {
	v := &filesystemValidator{out: out}

	if err := v.validateFile(top, ""); err != nil {
		return err
	}

	if err := v.printFile(top, "", ""); err != nil {
		return err
	}

	return v.validateAndDump(top, "", "  ")
}
