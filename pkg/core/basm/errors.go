package basm

import "errors"

// Validation error categories distinguish local acceptance bounds from malformed
// input and internally inconsistent peer claims. They are not trust verdicts.
var (
	ErrInvalidHash   = errors.New("invalid BASM hash")
	ErrInvalidInput  = errors.New("invalid BASM input")
	ErrLimitExceeded = errors.New("BASM local limit exceeded")
	ErrInconsistent  = errors.New("inconsistent BASM claim")
	ErrNoncontiguous = errors.New("noncontiguous TAC")
)
