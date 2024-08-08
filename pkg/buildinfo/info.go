//go:build official

package buildinfo

import _ "embed"

//go:embed commit.txt
var VERSION string
