package eventhost

import (
	"context"
	"time"
)

type Event interface{}

type Dispatcher interface {
	Dispatch(context.Context, Event)
}

type SMSReceived struct {
	DevID   string
	Sender  string
	Content string
	Time    time.Time
}

type SMSSent struct {
	DevID      string
	TargetURI  string
	Content    string
	Time       time.Time
	TotalParts int
}

type USSDUpdated struct {
	DevID     string
	SessionID string
	Text      string
	RawText   string
	Status    int
	DCS       int
	Done      bool
	Time      time.Time
}

type LocalNumberLearned struct {
	DevID  string
	IMSI   string
	Number string
	Source string
	Time   time.Time
}

type LogNotify struct {
	DevID   string
	Message string
	Time    time.Time
}
