package builder

import (
	"fmt"
	"io"

	"github.com/tinyrange/tinyrange/pkg/common"
	"gopkg.in/yaml.v3"
)

type serializedPlan struct {
	Builder  string   `yaml:"builder"`
	Packages []string `yaml:"packages"`
	Tags     []string `yaml:"tags"`
}

type serializedDirective struct {
	Plan       *serializedPlan `yaml:"plan,omitempty"`
	RunCommand *string         `yaml:"run_command,omitempty"`
}

type serializedBuildVm struct {
	Directives []serializedDirective `yaml:"directives"`
}

func UnmarshalDefinition(r io.Reader) (common.BuildDefinition, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var out serializedBuildVm

	if err := dec.Decode(&out); err != nil {
		return nil, err
	}

	var directives []common.Directive

	for _, dir := range out.Directives {
		if dir.Plan != nil {
			params := PlanParameters{
				Builder: dir.Plan.Builder,
			}

			for _, q := range dir.Plan.Packages {
				query, err := common.ParsePackageQuery(q)
				if err != nil {
					return nil, err
				}

				params.Search = append(params.Search, query)
			}

			for _, tag := range dir.Plan.Tags {
				params.TagList = append(params.TagList, tag)
			}

			directives = append(directives, &PlanDefinition{params: params})
		} else if dir.RunCommand != nil {
			directives = append(directives, common.DirectiveRunCommand{
				Command: *dir.RunCommand,
			})
		} else {
			return nil, fmt.Errorf("directive not implemented: %+v", dir)
		}
	}

	ret := NewBuildVmDefinition(directives, nil, nil, "", 0, 0, 0, "ssh")

	return ret, nil
}

func MarshalDefinition(w io.Writer, def common.BuildDefinition) error {
	var obj any

	switch def := def.(type) {
	case *BuildVmDefinition:
		objVm := serializedBuildVm{}

		for _, dir := range def.params.Directives {
			switch dir := dir.(type) {
			case *PlanDefinition:
				directive := serializedPlan{
					Builder: dir.params.Builder,
					Tags:    dir.params.TagList,
				}

				for _, q := range dir.params.Search {
					directive.Packages = append(directive.Packages, q.String())
				}

				objVm.Directives = append(objVm.Directives, serializedDirective{
					Plan: &directive,
				})
			case common.DirectiveRunCommand:
				objVm.Directives = append(objVm.Directives, serializedDirective{
					RunCommand: &dir.Command,
				})
			default:
				return fmt.Errorf("MarshalDefinition directive unimplemented: %T %+v", dir, dir)
			}
		}

		obj = objVm
	default:
		return fmt.Errorf("MarshalDefinition unimplemented: %T %+v", def, def)
	}

	enc := yaml.NewEncoder(w)

	if err := enc.Encode(&obj); err != nil {
		return err
	}

	return nil
}
