package remote

import "encoding/json"

type MessageKind string

type BaseMessage interface {
	TagBaseMessage()
	Kind() MessageKind
}

type SessionMessage struct {
	Kind     MessageKind
	Contents json.RawMessage
}
