package qmi

import (
	"testing"
)

func TestErrServiceNotSupported(t *testing.T) {
	err := ErrServiceNotSupported
	if err.Error() != "qmi service not supported by hardware" {
		t.Errorf("unexpected error message: %v", err)
	}
}
