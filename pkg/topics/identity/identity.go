// Package identity implements the opt-in tm_identity application rules and
// ls_identity lookup service. Transaction/script/SPV verification belongs to the
// engine and must precede admission. Certificate admission alone establishes
// neither transaction validity, unspentness, revocation status nor client trust.
//
// A host must inject a durable Projection and register both services explicitly.
// This package supplies no production storage or default server registration.
package identity

import (
	"errors"
)

const (
	// Topic is the identity certificate admission topic.
	Topic = "tm_identity"
	// Service is the public identity lookup service.
	Service = "ls_identity"
)

var (
	// ErrInvalidOutput means an output does not satisfy identity application rules.
	ErrInvalidOutput = errors.New("invalid identity output")
	// ErrCertificateVerification means the certifier signature could not be
	// verified using the Go SDK serialization profile. This may indicate an
	// invalid signature or an unsupported TS field-order profile; it does not
	// diagnose forgery. The package does not try alternative permutations.
	ErrCertificateVerification = errors.New("identity certificate verification failed under Go SDK serialization profile")
	// ErrAdmissionBudget means a configured application validation budget was exceeded.
	ErrAdmissionBudget = errors.New("identity admission budget exceeded")
	// ErrInvalidTransaction means the selected transaction is absent or malformed.
	ErrInvalidTransaction = errors.New("invalid identity transaction")
	// ErrInvalidPolicy means a service resource policy is unusable.
	ErrInvalidPolicy = errors.New("invalid identity policy")
)

// AdmissionPolicy bounds work on identity output scripts, separately from the
// host's transport, transaction graph, proof and engine admission budgets.
type AdmissionPolicy struct {
	MaxScriptBytes       int
	MaxFields            int
	MaxOutputs           int
	MaxTotalScriptBytes  int
	MaxNotificationBytes int
}

// DefaultAdmissionPolicy returns finite limits for this opt-in application.
// Larger payload deployments must select and test their own limits explicitly.
func DefaultAdmissionPolicy() AdmissionPolicy {
	return AdmissionPolicy{
		MaxScriptBytes:       1 << 20,
		MaxFields:            128,
		MaxOutputs:           10000,
		MaxTotalScriptBytes:  32 << 20,
		MaxNotificationBytes: 64 << 20,
	}
}

func (p AdmissionPolicy) validate() error {
	if p.MaxScriptBytes < 1 || p.MaxFields < 1 || p.MaxOutputs < 1 ||
		p.MaxTotalScriptBytes < p.MaxScriptBytes || p.MaxNotificationBytes < 1 {
		return ErrInvalidPolicy
	}
	return nil
}
