package identity

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

var (
	// ErrQueryBudget means a legacy unbounded query exceeded the result budget.
	// Callers must use explicit bounded limit/offset pagination; no partial
	// success or new wire response type is returned.
	ErrQueryBudget = errors.New("identity query budget exceeded; use bounded pagination")
	// ErrInvalidNotification means the engine notification cannot be projected.
	ErrInvalidNotification = errors.New("invalid identity notification")
	// ErrProjectionContract means the injected projection violated its contract.
	ErrProjectionContract = errors.New("identity projection contract violation")
)

// LookupService maintains the injected public identity projection and returns
// outpoint formulas for the engine's BEEF hydration. Register it explicitly as
// ls_identity; successful callbacks inherit the projection's durability.
type LookupService struct {
	projection      Projection
	admissionPolicy AdmissionPolicy
	queryPolicy     QueryPolicy
}

var _ engine.LookupService = (*LookupService)(nil)

// NewLookupService creates a service using projection and default resource
// policies. projection must be non-nil and implement the durability contract.
func NewLookupService(projection Projection) (*LookupService, error) {
	return NewLookupServiceWithPolicies(projection, DefaultAdmissionPolicy(), DefaultQueryPolicy())
}

// NewLookupServiceWithPolicies creates a service with explicit admission and
// query budgets. It performs no I/O or server registration and rejects nil
// projection implementations, including typed nil values.
func NewLookupServiceWithPolicies(projection Projection, admission AdmissionPolicy, query QueryPolicy) (*LookupService, error) {
	if nilProjection(projection) {
		return nil, fmt.Errorf("%w: projection is required", ErrProjectionContract)
	}
	if err := admission.validate(); err != nil {
		return nil, err
	}
	if err := query.validate(); err != nil {
		return nil, err
	}
	return &LookupService{projection: projection, admissionPolicy: admission, queryPolicy: query}, nil
}

func nilProjection(projection Projection) bool {
	if projection == nil {
		return true
	}
	value := reflect.ValueOf(projection)
	switch value.Kind() { //nolint:exhaustive // All nil-capable kinds are listed; every other kind is non-nil.
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// OutputAdmittedByTopic projects an engine-admitted tm_identity output from
// payload's atomic BEEF. Other topics are ignored. ctx flows into verification
// and the durable upsert; failures prevent a successful callback acknowledgment.
// The engine remains responsible for transaction/script/SPV validation and for
// atomic admission/outbox integration when configured by the host.
func (s *LookupService) OutputAdmittedByTopic(ctx context.Context, payload *engine.OutputAdmittedByTopic) error {
	if payload == nil {
		return ErrInvalidNotification
	}
	if payload.Topic != Topic {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(payload.AtomicBEEF) > s.admissionPolicy.MaxNotificationBytes {
		return ErrAdmissionBudget
	}
	beef, txid, err := parseNotificationBEEF(payload.AtomicBEEF)
	if err != nil {
		return err
	}
	tx, err := selectedTransaction(beef, txid, s.admissionPolicy)
	if err != nil {
		return err
	}
	if uint64(payload.OutputIndex) >= uint64(len(tx.Outputs)) || len(tx.Inputs) == 0 {
		return ErrInvalidNotification
	}
	record, err := ProjectOutput(ctx, transaction.Outpoint{Txid: *txid, Index: payload.OutputIndex}, tx.Outputs[payload.OutputIndex].LockingScript, s.admissionPolicy)
	if err != nil {
		return err
	}
	if err = s.projection.Upsert(ctx, record); err != nil {
		return fmt.Errorf("identity projection upsert: %w", err)
	}
	return nil
}

// parseNotificationBEEF only processes engine-produced atomic BEEF, not a
// transport boundary. The byte budget is checked by the caller. The SDK can
// panic on malformed BUMP indices; convert that into a failed notification.
func parseNotificationBEEF(raw []byte) (beef *transaction.Beef, txid *chainhash.Hash, err error) {
	defer func() {
		if recover() != nil {
			beef, txid, err = nil, nil, ErrInvalidNotification
		}
	}()
	beef, txid, err = transaction.NewBeefFromAtomicBytes(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidNotification, err)
	}
	return beef, txid, nil
}

// OutputSpent removes payload's outpoint when its topic is tm_identity. Other
// topics are ignored. Duplicate deletions must succeed in the projection.
func (s *LookupService) OutputSpent(ctx context.Context, payload *engine.OutputSpent) error {
	if payload == nil {
		return ErrInvalidNotification
	}
	if payload.Topic != Topic {
		return nil
	}
	return s.OutputEvicted(ctx, payload.Outpoint)
}

// OutputEvicted removes outpoint from all identity lookup answers through the
// injected projection. ctx cancels the delete; a nil outpoint is invalid.
func (s *LookupService) OutputEvicted(ctx context.Context, outpoint *transaction.Outpoint) error {
	if outpoint == nil {
		return ErrInvalidNotification
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.projection.Delete(ctx, *outpoint); err != nil {
		return fmt.Errorf("identity projection delete: %w", err)
	}
	return nil
}

// OutputNoLongerRetainedInHistory leaves the current-output projection intact:
// historical retention is distinct from spend/eviction. Only ctx is consulted;
// outpoint and topic require no identity projection mutation.
func (s *LookupService) OutputNoLongerRetainedInHistory(ctx context.Context, _ *transaction.Outpoint, _ string) error {
	return ctx.Err()
}

// OutputBlockHeightUpdated does not alter identity query fields. ctx is honored;
// txid, blockHeight and blockIndex remain the engine's chain-history concern.
func (s *LookupService) OutputBlockHeightUpdated(ctx context.Context, _ *chainhash.Hash, _ uint32, _ uint64) error {
	return ctx.Err()
}

// Lookup validates question and asks the projection for actual outpoints. ctx
// flows to storage. The result contains formulas, never fabricated BEEF. Native
// text semantics and local ordering are supplied by the projection contract.
func (s *LookupService) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if question == nil || question.Service != Service {
		return nil, ErrInvalidQuery
	}
	query, err := CompileQuery(question.Query, s.queryPolicy)
	if err != nil {
		return nil, err
	}
	answer := &lookup.LookupAnswer{Type: lookup.AnswerTypeFormula, Formulas: []lookup.LookupFormula{}}
	if query.Empty {
		return answer, nil
	}
	outpoints, err := s.projection.Find(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("identity projection query: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if query.Unbounded && len(outpoints) > query.ResultLimit {
		return nil, ErrQueryBudget
	}
	if len(outpoints) > query.Limit {
		return nil, fmt.Errorf("%w: result exceeds requested limit", ErrProjectionContract)
	}
	answer.Formulas = make([]lookup.LookupFormula, 0, len(outpoints))
	for _, outpoint := range outpoints {
		answer.Formulas = append(answer.Formulas, lookup.LookupFormula{Outpoint: &outpoint})
	}
	return answer, nil
}

// GetDocumentation describes the service and its explicit legacy query limit.
func (s *LookupService) GetDocumentation() string {
	return "Identity Lookup Service: find public identity certificates by serial number, attribute, identity key, certifier, or certificate type. Legacy unbounded results exceeding the configured budget require bounded pagination."
}

// GetMetaData returns a fresh metadata value for this service.
func (s *LookupService) GetMetaData() *overlay.MetaData {
	return &overlay.MetaData{Name: "Identity Lookup Service", Description: "Identity resolution made easy."}
}
