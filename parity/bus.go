package main

import (
	"sync"

	"github.com/nats-io/nats.go"
)

// busMsg is one message the checker heard: its subject, body and Nats-Msg-Id.
type busMsg struct {
	Subject string
	Data    []byte
	ID      string
}

// listener hears what is published on some subjects (R1). Taken returns what it
// has heard so far, in the order it heard it, and Close stops it.
type listener interface {
	Taken() []busMsg
	Close()
}

// listen subscribes to subjects on the server at url; tests swap it.
type listenFunc func(url string, subjects ...string) (listener, error)

// natsListener is a core subscription: it hears every event and callout as ecu
// publishes it, whichever stream keeps it, and CALLOUTS being a work queue does
// not matter to it (R1, R7: the same works on a live source).
type natsListener struct {
	nc   *nats.Conn
	mu   sync.Mutex
	msgs []busMsg
}

func natsListen(url string, subjects ...string) (listener, error) {
	nc, err := nats.Connect(url, nats.Name("parity"))
	if err != nil {
		return nil, err
	}
	l := &natsListener{nc: nc}
	for _, s := range subjects {
		if _, err := nc.Subscribe(s, l.hear); err != nil {
			nc.Close()
			return nil, err
		}
	}
	// The subscriptions are on the server before the replay starts.
	if err := nc.Flush(); err != nil {
		nc.Close()
		return nil, err
	}
	return l, nil
}

func (l *natsListener) hear(m *nats.Msg) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, busMsg{Subject: m.Subject, Data: m.Data, ID: m.Header.Get(nats.MsgIdHdr)})
}

func (l *natsListener) Taken() []busMsg {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]busMsg(nil), l.msgs...)
}

func (l *natsListener) Close() { l.nc.Close() }
