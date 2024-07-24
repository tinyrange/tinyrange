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
			plan := &PlanDefinition{
				Builder: dir.Plan.Builder,
			}

			for _, q := range dir.Plan.Packages {
				query, err := common.ParsePackageQuery(q)
				if err != nil {
					return nil, err
				}

				plan.Search = append(plan.Search, query)
			}

			for _, tag := range dir.Plan.Tags {
				plan.TagList = append(plan.TagList, tag)
			}

			directives = append(directives, plan)
		} else if dir.RunCommand != nil {
			directives = append(directives, common.DirectiveRunCommand(*dir.RunCommand))
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
					Builder: dir.Builder,
					Tags:    dir.TagList,
				}

				for _, q := range dir.Search {
					directive.Packages = append(directive.Packages, q.String())
				}

				objVm.Directives = append(objVm.Directives, serializedDirective{
					Plan: &directive,
				})
			case common.DirectiveRunCommand:
				objVm.Directives = append(objVm.Directives, serializedDirective{
					RunCommand: (*string)(&dir),
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
