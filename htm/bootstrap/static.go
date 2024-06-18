package bootstrap

import (
	_ "embed"
)

//go:embed bootstrap.bundle.min.js
var JavascriptSrcRaw string

//go:embed bootstrap.min.css
var CssSrcRaw string

//go:embed color-picker.js
var ColorPickerRaw string
