package internal

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"golang.org/x/term"
)

type event interface {
	tagEvent()
}

type baseEvent struct{}

func (baseEvent) tagEvent() {}

type newGroupEvent struct {
	baseEvent
	Parent string
	Name   string
}

type setGroupDescriptionEvent struct {
	baseEvent
	Group       string
	Description string
}

type logEvent struct {
	baseEvent
	Group   string
	Message string
}

type groupClosedEvent struct {
	baseEvent
	Group string
}

type group struct {
	sink   EventSink
	name   string
	closed bool
}

func (g *group) Description(format string, args ...interface{}) {
	g.sink.SendEvent(setGroupDescriptionEvent{Group: g.name, Description: fmt.Sprintf(format, args...)})
}

func (g *group) Logf(format string, args ...interface{}) {
	g.sink.SendEvent(logEvent{Group: g.name, Message: fmt.Sprintf(format, args...)})
}

func (g *group) Subgroup(name string) LogGroup {
	g.sink.SendEvent(newGroupEvent{Parent: g.name, Name: name})

	return &group{name: name, sink: g.sink}
}

func (g *group) Close() error {
	if g.closed {
		return nil
	}

	g.closed = true
	g.sink.SendEvent(groupClosedEvent{Group: g.name})

	return nil
}

var (
	_ LogGroup = &group{}
)

type logLine struct {
	timestamp time.Time
	line      string
	children  []*logLine
	closed    bool
}

func (l *logLine) render(out io.Writer, prefix string, height int, width int) int {
	totalHeight := 1
	for _, child := range l.children {
		if child.closed {
			fmt.Fprintf(out, "%s-- %s\n", prefix, child.line)
		} else {
			fmt.Fprintf(out, "%s%s\n", prefix, child.line)
		}
		totalHeight += child.render(out, prefix+"| ", height, width)
	}
	return totalHeight
}

type buildTui struct {
	FrameRate int // in frames per second

	events        chan event
	closed        chan struct{}
	consoleWidth  int
	consoleHeight int
	lastHeight    int
	output        io.Writer
	namedGroups   map[string]*logLine
	root          *logLine
}

func (b *buildTui) SendEvent(event event) {
	b.events <- event
}

func (b *buildTui) Group(name string) LogGroup {
	b.SendEvent(newGroupEvent{Name: name})
	return &group{
		sink: b,
		name: name,
	}
}

func (b *buildTui) Close() error {
	close(b.closed)

	return nil
}

func (b *buildTui) render() error {
	for i := 0; i < b.lastHeight-1; i++ {
		// clear the line
		fmt.Fprintf(b.output, "\033[2K\033[A\r")
	}

	b.lastHeight = b.root.render(b.output, "", b.consoleWidth, b.consoleHeight)

	return nil
}

func (b *buildTui) getGroup(name string) (*logLine, error) {
	group, ok := b.namedGroups[name]
	if !ok {
		return nil, fmt.Errorf("group not found: %s", name)
	}

	return group, nil
}

func (b *buildTui) processEvent(ev event) error {
	switch e := ev.(type) {
	case newGroupEvent:
		parent, err := b.getGroup(e.Parent)
		if err != nil {
			return err
		}

		parent.children = append(parent.children, &logLine{
			timestamp: time.Now(),
			line:      fmt.Sprintf("group: %s", e.Name),
		})

		b.namedGroups[e.Name] = parent.children[len(parent.children)-1]
	case logEvent:
		group, err := b.getGroup(e.Group)
		if err != nil {
			return err
		}

		group.children = append(group.children, &logLine{
			timestamp: time.Now(),
			line:      e.Message,
		})
	case setGroupDescriptionEvent:
		group, err := b.getGroup(e.Group)
		if err != nil {
			return err
		}

		group.line = e.Description
	case groupClosedEvent:
		group, err := b.getGroup(e.Group)
		if err != nil {
			return err
		}

		group.closed = true
	default:
		return fmt.Errorf("unknown event type: %T", ev)
	}
	return nil
}

type termSize struct {
	width  int
	height int
}

func (b *buildTui) Run(out io.Writer) error {
	b.output = out

	errChan := make(chan error, 1)
	resizeChan := make(chan termSize, 1)

	// get the width and height of the console
	if f, ok := out.(*os.File); ok {
		width, height, err := term.GetSize(int(f.Fd()))
		if err != nil {
			return fmt.Errorf("failed to get terminal size: %w", err)
		}
		b.consoleWidth = width
		b.consoleHeight = height
		go getAndWatchSize(int(f.Fd()), b.closed, errChan, resizeChan)
	}

	ticker := time.NewTicker(time.Second / time.Duration(b.FrameRate))

	for {
		select {
		case <-ticker.C:
			if err := b.render(); err != nil {
				return err
			}
		case event := <-b.events:
			if err := b.processEvent(event); err != nil {
				return err
			}
		case <-b.closed:
			return nil
		case ts := <-resizeChan:
			b.consoleWidth = ts.width
			b.consoleHeight = ts.height
		case err := <-errChan:
			return err
		}
	}
}

var (
	_ EventSink = &buildTui{}
)

func NewBuildLogger(eventBacklog int) Logger {
	tui := &buildTui{
		FrameRate:     30,
		consoleWidth:  80,
		consoleHeight: 24,
		closed:        make(chan struct{}),
		events:        make(chan event, eventBacklog),
		namedGroups:   make(map[string]*logLine),
	}

	tui.root = &logLine{
		timestamp: time.Now(),
		line:      "root",
	}

	tui.namedGroups[""] = tui.root

	return tui
}

type simpleLogger struct {
}

func (s *simpleLogger) SendEvent(ev event) {
	switch e := ev.(type) {
	case newGroupEvent:
		parentStr := e.Parent
		if parentStr == "" {
			parentStr = "root"
		}
		if len(parentStr) > 8 {
			parentStr = parentStr[:8]
		}
		slog.Info("New group", "parent", parentStr, "name", e.Name[:8])
	case setGroupDescriptionEvent:
		slog.Info("Set group description", "group", e.Group[:8], "description", e.Description)
	case logEvent:
		slog.Info("Log", "group", e.Group[:8], "message", e.Message)
	case groupClosedEvent:
		slog.Info("Group closed", "group", e.Group[:8])
	}
}

func (s *simpleLogger) Run(out io.Writer) error {
	return nil
}

func (s *simpleLogger) Group(name string) LogGroup {
	return &group{
		sink: s,
		name: name,
	}
}

func (s *simpleLogger) Close() error {
	return nil
}

var (
	_ EventSink = &simpleLogger{}
	_ Logger    = &simpleLogger{}
)

func NewSimpleLogger() Logger {
	return &simpleLogger{}
}

type EventSink interface {
	SendEvent(event)
}
