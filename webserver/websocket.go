package webserver

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

type KubeWebSocketMessage interface {
	Serialize() []byte
}

type message struct {
	Type   watch.EventType `json:"type"`
	Object runtime.Object  `json:"object"`
}

func (m message) Serialize() []byte {
	b, _ := json.Marshal(m)
	return b
}

func NewMessage(event watch.Event) KubeWebSocketMessage {
	return &message{
		Type:   event.Type,
		Object: event.Object,
	}
}
