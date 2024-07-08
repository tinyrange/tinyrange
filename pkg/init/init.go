package init

import _ "embed"

//go:embed init
var INIT_EXECUTABLE []byte

//go:embed init.star
var INIT_SCRIPT []byte
