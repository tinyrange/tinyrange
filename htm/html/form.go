package html

import (
	"fmt"

	"github.com/tinyrange/pkg2/v2/htm"
)

func Form(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("form", children...) }
func FormTarget(method string, target string) htm.Fragment {
	return htm.Group{
		htm.Attr("method", method),
		htm.Attr("action", target),
	}
}
func Button(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("button", children...) }
func Label(forId Id, text string, children ...htm.Fragment) htm.Fragment {
	childList := []htm.Fragment{
		htm.Attr("for", string(forId)),
		Text(text),
	}
	childList = append(childList, children...)
	return htm.NewHtmlFragment("label", childList...)
}

type FormFieldKind string

const (
	FormFieldText          FormFieldKind = "input"
	FormFieldNumber        FormFieldKind = "number"
	FormFieldCheckbox      FormFieldKind = "checkbox"
	FormFieldMultilineText FormFieldKind = "textarea"
	FormFieldSelect        FormFieldKind = "select"
)

type FormOptions struct {
	Kind          FormFieldKind
	Value         any
	Placeholder   string
	Required      bool
	LabelSameLine bool
	Options       []string
}

func boolToString(b bool) string {
	if b {
		return "true"
	} else {
		return "false"
	}
}

func FormField(label string, name string, opts FormOptions, children ...htm.Fragment) htm.Fragment {
	fieldId := NewId()
	var input htm.Fragment

	switch opts.Kind {
	case FormFieldNumber:
		input = htm.NewHtmlFragment("input",
			htm.Attr("type", "number"),
			htm.Attr("name", name),
			fieldId,
			htm.Attr("value", fmt.Sprintf("%d", opts.Value.(int))),
			htm.Attr("placeholder", opts.Placeholder),
		)
	case FormFieldCheckbox:
		var isChecked htm.Fragment
		if opts.Value.(bool) {
			isChecked = htm.Attr("checked", "")
		}
		input = htm.NewHtmlFragment("input",
			htm.Attr("type", "checkbox"),
			htm.Attr("name", name),
			fieldId,
			isChecked,
			htm.Attr("placeholder", opts.Placeholder),
		)
	case FormFieldMultilineText:
		input = htm.NewHtmlFragment("textarea",
			htm.Attr("name", name),
			htm.Attr("rows", "4"),
			htm.Attr("cols", "50"),
			fieldId,
			htm.Text(opts.Value.(string)),
			htm.Attr("placeholder", opts.Placeholder),
		)
	default:
		input = htm.NewHtmlFragment("input",
			htm.Attr("type", "input"),
			htm.Attr("name", name),
			fieldId,
			htm.Attr("value", opts.Value.(string)),
			htm.Attr("placeholder", opts.Placeholder),
		)
	}

	return Div(Label(fieldId, label), input)
}

func SubmitButton(text string) htm.Fragment {
	return htm.NewHtmlFragment("input",
		htm.Attr("type", "submit"),
		htm.Attr("value", text),
	)
}
