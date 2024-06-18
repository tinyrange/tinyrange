package database

import (
	"fmt"

	"github.com/tinyrange/pkg2/v2/common"
)

type InstallationPlan struct {
	Packages   []*common.Package
	Directives []common.Directive
}

func EmitDockerfile(plan *InstallationPlan) (string, error) {
	ret := ""

	for _, directive := range plan.Directives {
		switch directive := directive.(type) {
		case common.DirectiveBaseImage:
			ret += fmt.Sprintf("FROM %s\n", string(directive))
		case common.DirectiveRunCommand:
			ret += fmt.Sprintf("RUN %s\n", string(directive))
		default:
			return "", fmt.Errorf("directive %T not handled for docker", directive)
		}
	}

	return ret, nil
}
