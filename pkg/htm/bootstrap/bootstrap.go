package bootstrap

import (
	_ "embed"
	"fmt"

	"github.com/tinyrange/pkg2/pkg/htm"
	"github.com/tinyrange/pkg2/pkg/htm/html"
)

var CSSSrc = html.Style(CssSrcRaw)
var JavaScriptSrc = html.JavaScript(JavascriptSrcRaw)
var ColorPickerSrc = html.JavaScript(ColorPickerRaw)

var (
	Container      = htm.Class("container")
	ContainerFluid = htm.Class("container-fluid")
	Row            = htm.Class("row")
	Column         = htm.Class("col")
	ColumnMedium   = htm.Class("col-md")
	Column4        = htm.Class("col-4")
	Column6        = htm.Class("col-6")
	Column8        = htm.Class("col-8")
	Column4Medium  = htm.Class("col-md-4")
	Column6Medium  = htm.Class("col-md-6")
	Column8Medium  = htm.Class("col-md-8")
	Column4Large   = htm.Class("col-lg-4")
	Column6Large   = htm.Class("col-lg-6")
	Column8Large   = htm.Class("col-lg-8")
	DisplayFlex    = htm.Class("d-flex")
	Width100       = htm.Class("w-100")
)

func Card(body ...htm.Fragment) htm.Fragment {
	var childList []htm.Fragment
	childList = append(childList, htm.Class("card-body"))
	childList = append(childList, body...)
	return html.Div(htm.Class("card"),
		html.Div(childList...),
	)
}

func CardTitle(text string) htm.Fragment {
	return html.H5(htm.Class("card-title"), htm.Text(text))
}

var (
	ButtonSmall = htm.Class("btn-sm")
	ButtonLarge = htm.Class("btn-lg")
)

type ButtonColor string

const (
	ButtonColorPrimary   ButtonColor = "btn-primary"
	ButtonColorSecondary ButtonColor = "btn-secondary"
	ButtonColorSuccess   ButtonColor = "btn-success"
	ButtonColorDanger    ButtonColor = "btn-danger"
	ButtonColorWarning   ButtonColor = "btn-warning"
	ButtonColorInfo      ButtonColor = "btn-info"
	ButtonColorLight     ButtonColor = "btn-light"
	ButtonColorDark      ButtonColor = "btn-dark"
	ButtonColorLink      ButtonColor = "btn-link"
)

func ButtonClass(color ButtonColor) htm.Group {
	return htm.Group{htm.Class("btn"), htm.Class(color)}
}

func Button(color ButtonColor, children ...htm.Fragment) htm.Fragment {
	var childList []htm.Fragment
	childList = append(childList, htm.Class("btn"), htm.Class(color))
	childList = append(childList, children...)
	return html.Button(childList...)
}

func ButtonA(color ButtonColor, children ...htm.Fragment) htm.Fragment {
	var childList []htm.Fragment
	childList = append(childList, htm.Class("btn"), htm.Class(color))
	childList = append(childList, children...)
	return html.A(childList...)
}

func LinkButton(target string, color ButtonColor, children ...htm.Fragment) htm.Fragment {
	var childList []htm.Fragment
	childList = append(childList, htm.Class("btn"), htm.Class(color))
	childList = append(childList, children...)
	return html.Link(target, childList...)
}

func FormField(label string, name string, opts html.FormOptions, children ...htm.Fragment) htm.Fragment {
	fieldId := html.NewId()
	var input htm.Fragment

	var requiredFrag htm.Fragment

	if opts.Required {
		requiredFrag = htm.Attr("required", "")
	}

	switch opts.Kind {
	case html.FormFieldNumber:
		input = htm.NewHtmlFragment("input",
			htm.Attr("type", "number"),
			htm.Attr("name", name),
			htm.Class("form-control"),
			fieldId,
			htm.Attr("value", fmt.Sprintf("%d", opts.Value.(int))),
			htm.Attr("placeholder", opts.Placeholder),
			requiredFrag,
		)
	case html.FormFieldCheckbox:
		var isChecked htm.Fragment
		if opts.Value.(bool) {
			isChecked = htm.Attr("checked", "")
		}
		return html.Div(htm.Class("form-check"),
			htm.NewHtmlFragment("input",
				htm.Attr("type", "checkbox"),
				htm.Attr("name", name),
				isChecked,
				fieldId,
				htm.Class("form-check-input"),
			),
			html.Label(fieldId, label, htm.Class("form-check-label")),
		)
	case html.FormFieldMultilineText:
		input = htm.NewHtmlFragment("textarea",
			htm.Attr("name", name),
			htm.Attr("rows", "4"),
			htm.Attr("cols", "50"),
			htm.Class("form-control"),
			fieldId,
			htm.Text(opts.Value.(string)),
			htm.Attr("placeholder", opts.Placeholder),
			requiredFrag,
		)
	case html.FormFieldSelect:
		var options htm.Group
		for _, opt := range opts.Options {
			if opts.Value.(string) == opt {
				options = append(options,
					htm.NewHtmlFragment("option",
						htm.Attr("value", opt),
						htm.Attr("selected", ""),
						html.Text(opt),
					),
				)
			} else {
				options = append(options,
					htm.NewHtmlFragment("option",
						htm.Attr("value", opt),
						html.Text(opt),
					),
				)
			}
		}

		input = htm.NewHtmlFragment("select",
			htm.Attr("name", name),
			htm.Class("form-select"),
			fieldId,
			options,
		)
	default:
		input = htm.NewHtmlFragment("input",
			htm.Attr("type", "input"),
			htm.Attr("name", name),
			htm.Class("form-control"),
			fieldId,
			htm.Attr("value", opts.Value.(string)),
			htm.Attr("placeholder", opts.Placeholder),
			requiredFrag,
		)
	}

	if opts.LabelSameLine {
		return html.Div(Row, htm.Class("mb-3"),
			html.Label(fieldId, label, htm.Class("form-label"), htm.Class("col-md-2")),
			html.Div(
				htm.Class("col-md-10"),
				input,
			),
		)
	} else {
		return html.Div(Row, htm.Class("mb-3"),
			html.Label(fieldId, label, htm.Class("form-label")),
			input,
		)
	}
}

func Table(headerRow htm.Group, rows []htm.Group, children ...htm.Fragment) htm.Fragment {
	var headerItems []htm.Fragment
	for _, item := range headerRow {
		headerItems = append(headerItems, htm.NewHtmlFragment("th", item))
	}
	var rowItems []htm.Fragment
	for _, item := range rows {
		var row htm.Group
		for _, cell := range item {
			row = append(row, htm.NewHtmlFragment("td", cell))
		}
		rowItems = append(rowItems, htm.NewHtmlFragment("tr", row))
	}

	return html.Div(
		htm.Class("table-responsive"),
		htm.Group(children),
		htm.NewHtmlFragment("table",
			htm.Class("table"),
			htm.NewHtmlFragment("thead",
				htm.NewHtmlFragment("tr", headerItems...),
			),
			htm.NewHtmlFragment("tbody", rowItems...),
		),
	)
}

func NavbarLink(target string, body htm.Fragment) htm.Fragment {
	return htm.NewHtmlFragment("li", htm.Class("nav-link"),
		html.Link(target, htm.Class("nav-link"), body),
	)
}

func NavbarBrand(target string, body htm.Fragment) htm.Fragment {
	return html.Link(target, htm.Class("navbar-brand"), body)
}

func Navbar(brand htm.Fragment, links ...htm.Fragment) htm.Fragment {
	return htm.NewHtmlFragment("nav", htm.Class("navbar"), htm.Class("navbar-expand-lg"), htm.Class("bg-body-tertiary"),
		html.Div(htm.Class("container-fluid"),
			brand,
			htm.NewHtmlFragment("button",
				htm.Class("navbar-toggler"),
				htm.Attr("type", "button"),
				htm.Attr("data-bs-toggle", "collapse"),
				htm.Attr("data-bs-target", "#navbarSupportedContent"),
				htm.NewHtmlFragment("span", htm.Class("navbar-toggler-icon")),
			),
			html.Div(htm.Class("collapse"), htm.Class("navbar-collapse"), html.Id("navbarSupportedContent"),
				htm.NewHtmlFragment("ul", htm.Class("navbar-nav"), htm.Class("me-auto"), htm.Class("mb-2"), htm.Class("mb-lg-0"),
					htm.Group(links),
				),
			),
		),
	)
}

func SubmitButton(text string, color ButtonColor, children ...htm.Fragment) htm.Fragment {
	var childList []htm.Fragment
	childList = append(childList,
		htm.Class("btn"),
		htm.Class(color),
		htm.Attr("type", "submit"),
		html.Textf("%s", text),
	)
	childList = append(childList, children...)
	return htm.NewHtmlFragment("button", childList...)
}
