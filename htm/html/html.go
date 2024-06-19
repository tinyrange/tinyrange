package html

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"

	"github.com/tinyrange/pkg2/v2/htm"
)

func Text(s string) htm.Fragment                 { return htm.Text(s) }
func Textf(format string, a ...any) htm.Fragment { return Text(fmt.Sprintf(format, a...)) }

func Html(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("html", children...) }
func Head(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("head", children...) }
func Body(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("body", children...) }
func Div(children ...htm.Fragment) htm.Fragment  { return htm.NewHtmlFragment("div", children...) }
func Span(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("span", children...) }
func Pre(children ...htm.Fragment) htm.Fragment  { return htm.NewHtmlFragment("pre", children...) }
func Code(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("code", children...) }
func A(children ...htm.Fragment) htm.Fragment    { return htm.NewHtmlFragment("a", children...) }

func H1(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("h1", children...) }
func H2(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("h2", children...) }
func H3(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("h3", children...) }
func H4(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("h4", children...) }
func H5(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("h5", children...) }
func H6(children ...htm.Fragment) htm.Fragment { return htm.NewHtmlFragment("h6", children...) }

func Link(target string, children ...htm.Fragment) htm.Fragment {
	childList := []htm.Fragment{
		htm.Attr("href", target),
	}
	childList = append(childList, children...)
	return A(childList...)
}

func LinkCSS(url string, children ...htm.Fragment) htm.Fragment {
	childList := []htm.Fragment{
		htm.Attr("rel", "stylesheet"),
		htm.Attr("href", url),
	}
	childList = append(childList, children...)
	return htm.NewHtmlFragment("link", childList...)
}

func Style(content string, children ...htm.Fragment) htm.Fragment {
	childList := []htm.Fragment{
		htm.Text(content),
	}
	childList = append(childList, children...)
	return htm.NewHtmlFragment("style", childList...)
}

func JavaScript(source string, children ...htm.Fragment) htm.Fragment {
	childList := []htm.Fragment{htm.Text(source)}
	childList = append(childList, children...)
	return htm.NewHtmlFragment("script", childList...)
}

func JavaScriptSrc(url string, children ...htm.Fragment) htm.Fragment {
	childList := []htm.Fragment{htm.Attr("src", url)}
	childList = append(childList, children...)
	return htm.NewHtmlFragment("script", childList...)
}

func Title(title string) htm.Fragment { return htm.NewHtmlFragment("title", htm.Text(title)) }

func (i Id) Children(ctx context.Context) ([]htm.Fragment, error) {
	return htm.Attr("id", string(i)).Children(ctx)
}
func (i Id) Render(ctx context.Context, parent htm.Node) error {
	return htm.Attr("id", string(i)).Render(ctx, parent)
}

var (
	_ htm.Fragment = Id("")
)

func NewId() Id {
	return Id("i" + strconv.FormatUint(rand.Uint64(), 36))
}

func MetaCharset(charset string) htm.Fragment {
	return htm.NewHtmlFragment("meta", htm.Attr("charset", charset))
}

func MetaViewport(value string) htm.Fragment {
	return htm.NewHtmlFragment("meta",
		htm.Attr("name", "viewport"),
		htm.Attr("content", value),
	)
}

func HiddenFormField(id Id, name string, value string) htm.Fragment {
	return htm.NewHtmlFragment("input",
		htm.Attr("type", "hidden"),
		id,
		htm.Attr("name", name),
		htm.Attr("value", value),
	)
}
