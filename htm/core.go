package htm

import "context"

type Node interface {
	AddClass(class string) error
	AddChild(node Node) error
	AddAttribute(key string, value string) error
	UnsafeAddRawHTML(content []byte) error
}

type Fragment interface {
	Children(ctx context.Context) ([]Fragment, error)
	Render(ctx context.Context, parent Node) error
}
