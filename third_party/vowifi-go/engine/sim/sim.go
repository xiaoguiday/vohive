package sim

import "errors"

var (
	ErrSyncFailure = errors.New("aka sync failure")
	ErrAuthFailure = errors.New("aka authentication failure")
)

type AKAResult struct {
	RES  []byte
	CK   []byte
	IK   []byte
	AUTS []byte
}

type AKAProvider interface {
	CalculateAKA(rand16, autn16 []byte) (AKAResult, error)
}

type ISIMAKAProvider interface {
	CalculateISIMAKA(rand16, autn16 []byte) (AKAResult, error)
}
