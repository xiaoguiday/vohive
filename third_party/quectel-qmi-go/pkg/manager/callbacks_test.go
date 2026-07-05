package manager

import "testing"

func TestEventEmitterEmitAfterCloseDoesNotBlockOrPanic(t *testing.T) {
	e := NewEventEmitterWithQueueSize(1)
	e.Close()

	if ok := e.Emit(Event{Type: EventDisconnected}); ok {
		t.Fatal("Emit after Close returned true, want false")
	}
}

func TestEventEmitterDropsWhenQueueFull(t *testing.T) {
	e := &EventEmitter{
		queue: make(chan Event, 1),
		done:  make(chan struct{}),
	}

	if ok := e.Emit(Event{Type: EventConnected}); !ok {
		t.Fatal("first Emit returned false, want true")
	}
	if ok := e.Emit(Event{Type: EventDisconnected}); ok {
		t.Fatal("second Emit returned true with full queue, want false")
	}
	if dropped := e.Dropped(); dropped != 1 {
		t.Fatalf("Dropped=%d want 1", dropped)
	}
}
