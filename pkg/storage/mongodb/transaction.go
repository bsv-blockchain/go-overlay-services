package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	// ErrCommitPending means a commit may have succeeded and must be reconciled.
	// A caller must not interpret it as an aborted transaction.
	ErrCommitPending = errors.New("MongoDB transaction commit is unresolved")
	errRetryBudget   = errors.New("MongoDB transaction retry budget exhausted")
)

type transactionOutcome uint8

const (
	transactionAborted transactionOutcome = iota
	transactionCommitted
	transactionPending
)

// runTransaction is deliberately the Core API: WithTransaction removes commit
// deadlines and can restart bodies after commit errors. Here an ambiguous
// commit permanently prohibits a fresh body, even if a later retry returns a
// different error. A pending outcome is reconciled through a durable guard.
func (s *Store) runTransaction(ctx context.Context, body func(context.Context) error) (transactionOutcome, error) {
	if mongo.SessionFromContext(ctx) != nil {
		return transactionAborted, ErrNestedTransaction
	}
	session, err := s.client.StartSession()
	if err != nil {
		return transactionAborted, err
	}
	outcome := transactionAborted
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if outcome == transactionPending {
			// EndSession otherwise sends an implicit abort after a commit timeout.
			// A canceled cleanup context releases local resources without sending a
			// competing abort. Neither cleanup nor lease expiry proves an abort.
			cancel()
		}
		session.EndSession(cleanupCtx)
	}()
	workCtx, cancel := context.WithTimeout(ctx, s.config.TransactionTimeout)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < s.config.MaxBodyAttempts; attempt++ {
		if err = workCtx.Err(); err != nil {
			return transactionAborted, err
		}
		if err = session.StartTransaction(s.transactionOptions()); err != nil {
			return transactionAborted, err
		}
		bodyCtx, bodyCancel := context.WithTimeout(workCtx, s.config.TransactionTimeout)
		bodyErr := body(mongo.NewSessionContext(bodyCtx, session))
		bodyCancel()
		if bodyErr != nil {
			abortCtx, abortCancel := context.WithTimeout(context.WithoutCancel(workCtx), 2*time.Second)
			abortErr := session.AbortTransaction(abortCtx)
			abortCancel()
			if abortErr != nil {
				return transactionAborted, errors.Join(bodyErr, abortErr)
			}
			lastErr = bodyErr
			if hasErrorLabel(bodyErr, "TransientTransactionError") {
				continue
			}
			return transactionAborted, bodyErr
		}
		commitBudget, commitCancel := context.WithTimeout(context.WithoutCancel(workCtx), s.config.CommitTimeout)
		ambiguous := false
		retryBody := false
		for commitAttempt := 0; commitAttempt < s.config.MaxCommitAttempts; commitAttempt++ {
			commitCtx, attemptCancel := context.WithTimeout(commitBudget, s.config.CommitTimeout/time.Duration(s.config.MaxCommitAttempts))
			commitErr := session.CommitTransaction(commitCtx)
			attemptCancel()
			if commitErr == nil {
				commitCancel()
				outcome = transactionCommitted
				return outcome, nil
			}
			lastErr = commitErr
			if hasErrorLabel(commitErr, "UnknownTransactionCommitResult") || mongo.IsTimeout(commitErr) || errors.Is(commitErr, context.Canceled) || mongo.IsNetworkError(commitErr) {
				ambiguous = true
			}
			if !ambiguous && hasErrorLabel(commitErr, "TransientTransactionError") {
				retryBody = true
				break
			}
			if !ambiguous {
				// Even unlabeled commit failures are conservatively unresolved; only an
				// explicit transient-abort label authorizes rerunning the body.
				ambiguous = true
			}
			if commitBudget.Err() != nil {
				break
			}
		}
		commitCancel()
		if retryBody {
			continue
		}
		outcome = transactionPending
		return outcome, errors.Join(ErrCommitPending, lastErr)
	}
	return transactionAborted, fmt.Errorf("%w: %w", errRetryBudget, lastErr)
}

func hasErrorLabel(err error, label string) bool {
	var labeled interface{ HasErrorLabel(label string) bool }
	return errors.As(err, &labeled) && labeled.HasErrorLabel(label)
}
