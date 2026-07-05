package manager

import (
	"testing"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func TestShouldLogRawIndicationSuppressesRoutineUIMSessionClosed(t *testing.T) {
	if shouldLogRawIndication(qmi.Event{Type: qmi.EventUIMSessionClosed}) {
		t.Fatal("routine UIM session closed indication should not emit raw debug log")
	}
	if !shouldLogRawIndication(qmi.Event{Type: qmi.EventUnknown}) {
		t.Fatal("unknown indication should keep raw debug log")
	}
}
