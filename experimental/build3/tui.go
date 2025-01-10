package main

import (
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/google/uuid"
)

type RootLogger interface {
	io.Closer
	Run(w io.Writer) error
	Group(name string) Logger
}

type eventDrivenGroup struct {
	groupStream chan *eventDrivenGroup
	lastUpdate  time.Time
	parentChain []string
	id          string
	description string
	closed      bool
	lowPriority bool
}

func (g *eventDrivenGroup) Close() error {
	if g.closed {
		return fmt.Errorf("group already closed")
	}

	g.closed = true

	return nil
}

func (g *eventDrivenGroup) Describe(format string, args ...interface{}) {
	g.description = fmt.Sprintf(format, args...)
	g.lastUpdate = time.Now()

	g.groupStream <- g
}

func (g *eventDrivenGroup) Logf(format string, args ...interface{}) {
	newGroup := &eventDrivenGroup{
		groupStream: g.groupStream,
		lastUpdate:  time.Now(),
		id:          uuid.NewString(),
		description: fmt.Sprintf(format, args...),
		parentChain: append(g.parentChain, g.id),
		lowPriority: true,
	}

	g.groupStream <- newGroup
}

func (g *eventDrivenGroup) Child(name string) Logger {
	newGroup := &eventDrivenGroup{
		groupStream: g.groupStream,
		lastUpdate:  time.Now(),
		id:          uuid.NewString(),
		description: name,
		parentChain: append(g.parentChain, g.id),
	}

	g.groupStream <- newGroup

	return newGroup
}

var (
	_ Logger = &eventDrivenGroup{}
)

type eventDrivenLogger struct {
	maxHeight   int
	groupStream chan *eventDrivenGroup

	currentEvents []*eventDrivenGroup

	lastPrintedLineCount int

	closed chan struct{}
}

func (l *eventDrivenLogger) addEvent(group *eventDrivenGroup) error {
	// Scan current events to see if the group already exists.
	for _, existingGroup := range l.currentEvents {
		if existingGroup.id == group.id {
			return nil
		}
	}

	// Add the group to the list.
	l.currentEvents = append(l.currentEvents, group)

	// Find the oldest group that's not a parent of the new group.
	if len(l.currentEvents) > l.maxHeight {
		dependChain := append(group.parentChain, group.id)
		// try to prune a low priority group.
		for i, existingGroup := range l.currentEvents {
			if existingGroup.lowPriority && !slices.Contains(dependChain, existingGroup.id) {
				// Remove the group.
				l.currentEvents = append(l.currentEvents[:i], l.currentEvents[i+1:]...)
				return nil
			}
		}

		// try to prune a closed group first.
		for i, existingGroup := range l.currentEvents {
			if existingGroup.closed && !slices.Contains(dependChain, existingGroup.id) {
				// Remove the group.
				l.currentEvents = append(l.currentEvents[:i], l.currentEvents[i+1:]...)
				return nil
			}
		}

		// try to prune a group that's not a parent of the new group.
		for i, existingGroup := range l.currentEvents {
			if !slices.Contains(dependChain, existingGroup.id) {
				// Remove the group.
				l.currentEvents = append(l.currentEvents[:i], l.currentEvents[i+1:]...)
				return nil
			}
		}

		// otherwise just remove the first unconditionally.
		l.currentEvents = l.currentEvents[1:]
	}

	return nil
}

type printTree struct {
	line     string
	children []*printTree
}

func (p *printTree) render(w io.Writer, prefix string) int {
	i := 0

	fmt.Fprintf(w, "%s%s\n", prefix, p.line)
	i++

	for _, child := range p.children {
		i += child.render(w, prefix+"  ")
	}

	return i
}

func (l *eventDrivenLogger) render(w io.Writer) error {
	for i := 0; i < l.lastPrintedLineCount; i++ {
		// clear the line
		fmt.Fprintf(w, "\033[2K\033[A\r")
	}

	// build a print tree from the current events
	printTreeMap := make(map[string]*printTree)
	root := &printTree{}

	for _, group := range l.currentEvents {
		if len(group.parentChain) > 0 {
			parent, ok := printTreeMap[group.parentChain[len(group.parentChain)-1]]
			if ok {
				printTreeMap[group.id] = &printTree{
					line: group.description,
				}
				parent.children = append(parent.children, printTreeMap[group.id])
				continue
			}
		}

		printTreeMap[group.id] = &printTree{
			line: group.description,
		}
		root.children = append(root.children, printTreeMap[group.id])
	}

	l.lastPrintedLineCount = 0
	for _, child := range root.children {
		l.lastPrintedLineCount += child.render(w, "")
	}

	return nil
}

func (l *eventDrivenLogger) Run(w io.Writer) error {
	frameRate := 30

	ticker := time.NewTicker(time.Second / time.Duration(frameRate))

	for {
		select {
		case group := <-l.groupStream:
			if group == nil {
				continue
			}
			if err := l.addEvent(group); err != nil {
				return err
			}
		case <-ticker.C:
			if err := l.render(w); err != nil {
				return err
			}
		case <-l.closed:
			return nil
		}
	}
}

func (l *eventDrivenLogger) Close() error {
	close(l.groupStream)
	return nil
}

func (l *eventDrivenLogger) Group(name string) Logger {
	newGroup := &eventDrivenGroup{
		groupStream: l.groupStream,
		lastUpdate:  time.Now(),
		id:          uuid.NewString(),
		description: name,
	}

	l.groupStream <- newGroup

	return newGroup
}

var (
	_ RootLogger = &eventDrivenLogger{}
)

func NewEventDrivenLogger(maxHeight int) RootLogger {
	return &eventDrivenLogger{
		maxHeight:     maxHeight,
		groupStream:   make(chan *eventDrivenGroup, 8),
		currentEvents: make([]*eventDrivenGroup, 0, maxHeight+1),
	}
}
