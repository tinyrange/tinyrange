package htm

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"strings"

	"golang.org/x/net/html"
)

type htmlNode struct {
	node      *html.Node
	classList []string
}

func (n *htmlNode) UnsafeAddRawHTML(content []byte) error {
	n.node.AppendChild(&html.Node{Type: html.RawNode, Data: string(content)})

	return nil
}

// AddClass implements Node.
func (n *htmlNode) AddClass(class string) error {
	n.classList = append(n.classList, class)

	for i, attr := range n.node.Attr {
		if attr.Key == "class" {
			attr.Val = strings.Join(n.classList, " ")
			n.node.Attr[i] = attr
			return nil
		}
	}

	n.node.Attr = append(n.node.Attr, html.Attribute{
		Key: "class",
		Val: strings.Join(n.classList, " "),
	})

	return nil
}

// AddAttribute implements Node.
func (n *htmlNode) AddAttribute(key string, value string) error {
	if n.node.Type == html.TextNode {
		return fmt.Errorf("cannot add attributes to text nodes")
	}

	n.node.Attr = append(n.node.Attr, html.Attribute{Key: key, Val: value})

	return nil
}

// AddChild implements Node.
func (n *htmlNode) AddChild(node Node) error {
	if n.node.Type == html.TextNode {
		return fmt.Errorf("cannot add attributes to text nodes")
	}

	newChild := node.(*htmlNode).node
	n.node.AppendChild(newChild)

	return nil
}

var (
	_ Node = &htmlNode{}
)

func newHtmlNode(tag string) Node {
	return &htmlNode{node: &html.Node{Type: html.ElementNode, Data: tag}}
}

type htmlFragment struct {
	tag      string
	children []Fragment
}

// Children implements Fragment.
func (h *htmlFragment) Children(ctx context.Context) ([]Fragment, error) {
	return h.children, nil
}

// Render implements Fragment.
func (h *htmlFragment) Render(ctx context.Context, parent Node) error {
	newNode := newHtmlNode(h.tag)

	for _, child := range h.children {
		if child == nil {
			continue
		}

		err := child.Render(ctx, newNode)
		if err != nil {
			return err
		}
	}

	return parent.AddChild(newNode)
}

var (
	_ Fragment = &htmlFragment{}
)

func NewHtmlFragment(tag string, children ...Fragment) Fragment {
	return &htmlFragment{tag: tag, children: children}
}

type Group []Fragment

// Children implements Fragment.
func (g Group) Children(ctx context.Context) ([]Fragment, error) {
	return g, nil
}

// Render implements Fragment.
func (g Group) Render(ctx context.Context, parent Node) error {
	for _, child := range g {
		if child == nil {
			continue
		}

		err := child.Render(ctx, parent)
		if err != nil {
			return err
		}
	}
	return nil
}

var (
	_ Fragment = Group{}
)

type Text string

func (Text) Children(ctx context.Context) ([]Fragment, error) {
	return []Fragment{}, nil
}

// Render implements Fragment.
func (t Text) Render(ctx context.Context, parent Node) error {
	return parent.AddChild(&htmlNode{node: &html.Node{Type: html.TextNode, Data: string(t)}})
}

var (
	_ Fragment = Text("")
)

type attr struct {
	key   string
	value string
}

// Children implements Fragment.
func (*attr) Children(ctx context.Context) ([]Fragment, error) {
	return []Fragment{}, nil
}

// Render implements Fragment.
func (a *attr) Render(ctx context.Context, parent Node) error {
	return parent.AddAttribute(a.key, a.value)
}

var (
	_ Fragment = &attr{}
)

func Attr(key string, value string) Fragment {
	return &attr{key: key, value: value}
}

type DynamicFunc func(ctx context.Context) ([]Fragment, error)

type dynamic struct {
	handler DynamicFunc
	id      uint64
}

// Children implements Fragment.
func (d *dynamic) Children(ctx context.Context) ([]Fragment, error) {
	// Dynamic trees hide their children so can't create routes.
	return []Fragment{}, nil
}

// Render implements Fragment.
func (d *dynamic) Render(ctx context.Context, parent Node) error {
	children, err := d.handler(ctx)
	if err != nil {
		return err
	}

	for _, child := range children {
		err := child.Render(ctx, parent)
		if err != nil {
			return err
		}
	}
	return nil
}

var (
	_ Fragment = &dynamic{}
)

func Dynamic(handler DynamicFunc) Fragment {
	return &dynamic{handler: handler, id: rand.Uint64()}
}

type Class string

// Children implements Fragment.
func (Class) Children(ctx context.Context) ([]Fragment, error) {
	return []Fragment{}, nil
}

// Render implements Fragment.
func (c Class) Render(ctx context.Context, parent Node) error {
	parent.AddClass(string(c))

	return nil
}

var (
	_ Fragment = Class("")
)

type UnsafeRawHTML []byte

// Children implements Fragment.
func (UnsafeRawHTML) Children(ctx context.Context) ([]Fragment, error) {
	return []Fragment{}, nil
}

// Render implements Fragment.
func (u UnsafeRawHTML) Render(ctx context.Context, parent Node) error {
	return parent.UnsafeAddRawHTML(u)
}

var (
	_ Fragment = UnsafeRawHTML{}
)

type topLevel struct {
	top Node
}

// UnsafeAddRawHTML implements Node.
func (*topLevel) UnsafeAddRawHTML(content []byte) error {
	return fmt.Errorf("cannot add raw html to top level node")
}

// AddClass implements Node.
func (*topLevel) AddClass(class string) error {
	return fmt.Errorf("cannot add classes to a top level node")
}

// AddAttribute implements Node.
func (*topLevel) AddAttribute(key string, value string) error {
	return fmt.Errorf("cannot add attributes to a top level node")
}

// AddChild implements Node.
func (t *topLevel) AddChild(node Node) error {
	if t.top == nil {
		t.top = node
		return nil
	} else {
		return fmt.Errorf("cannot have multiple top level nodes")
	}
}

var (
	_ Node = &topLevel{}
)

func WalkTree(ctx context.Context, frag Fragment) error {
	children, err := frag.Children(ctx)
	if err != nil {
		return err
	}

	for _, child := range children {
		err := WalkTree(ctx, child)
		if err != nil {
			return err
		}
	}

	return nil
}

func Render(ctx context.Context, w io.Writer, frag Fragment) error {
	if group, ok := frag.(Group); ok {
		for _, frag := range group {
			err := Render(ctx, w, frag)
			if err != nil {
				return err
			}
		}

		return nil
	} else {
		tree, err := RenderTree(ctx, frag)
		if err != nil {
			return err
		}

		return WriteTree(w, tree)
	}
}

func RenderTree(ctx context.Context, frag Fragment) (Node, error) {
	top := &topLevel{top: nil}

	err := frag.Render(ctx, top)
	if err != nil {
		return nil, err
	}

	return top.top, nil
}

func WriteTree(w io.Writer, tree Node) error {
	node := tree.(*htmlNode).node

	err := html.Render(w, node)
	if err != nil {
		return err
	}

	return nil
}
