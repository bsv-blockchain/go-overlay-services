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
	defer endTransactionSession(ctx, session, &outcome)
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
		bodyErr := runTransactionBody(workCtx, session, body, s.config.TransactionTimeout)
		if bodyErr != nil {
			retry, failErr := handleBodyFailure(workCtx, session, bodyErr)
			if retry {
				lastErr = failErr
				continue
			}
			return transactionAborted, failErr
		}
		commitOutcome, retry, commitErr := s.commitTransactionAttempt(workCtx, session)
		if retry {
			lastErr = commitErr
			continue
		}
		outcome = commitOutcome
		return outcome, commitErr
	}
	return transactionAborted, fmt.Errorf("%w: %w", errRetryBudget, lastErr)
}

func endTransactionSession(ctx context.Context, session *mongo.Session, outcome *transactionOutcome) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if *outcome == transactionPending {
		// EndSession otherwise sends an implicit abort after a commit timeout.
		// A canceled cleanup context releases local resources without sending a
		// competing abort. Neither cleanup nor lease expiry proves an abort.
		cancel()
	}
	session.EndSession(cleanupCtx)
}

func runTransactionBody(workCtx context.Context, session *mongo.Session, body func(context.Context) error, timeout time.Duration) error {
	bodyCtx, bodyCancel := context.WithTimeout(workCtx, timeout)
	defer bodyCancel()
	return body(mongo.NewSessionContext(bodyCtx, session))
}

func handleBodyFailure(workCtx context.Context, session *mongo.Session, bodyErr error) (bool, error) {
	retry, abortErr := abortTransactionAttempt(workCtx, session, bodyErr)
	if abortErr != nil {
		return false, abortErr
	}
	return retry, bodyErr
}

func abortTransactionAttempt(workCtx context.Context, session *mongo.Session, bodyErr error) (bool, error) {
	abortCtx, abortCancel := context.WithTimeout(context.WithoutCancel(workCtx), 2*time.Second)
	defer abortCancel()
	if abortErr := session.AbortTransaction(abortCtx); abortErr != nil {
		return false, errors.Join(bodyErr, abortErr)
	}
	return hasErrorLabel(bodyErr, "TransientTransactionError"), nil
}

func (s *Store) commitTransactionAttempt(workCtx context.Context, session *mongo.Session) (transactionOutcome, bool, error) {
	commitBudget, commitCancel := context.WithTimeout(context.WithoutCancel(workCtx), s.config.CommitTimeout)
	defer commitCancel()
	ambiguous := false
	var lastErr error
	for commitAttempt := 0; commitAttempt < s.config.MaxCommitAttempts; commitAttempt++ {
		commitCtx, attemptCancel := context.WithTimeout(commitBudget, s.config.CommitTimeout/time.Duration(s.config.MaxCommitAttempts))
		commitErr := session.CommitTransaction(commitCtx)
		attemptCancel()
		if commitErr == nil {
			return transactionCommitted, false, nil
		}
		lastErr = commitErr
		if unknownCommitResult(commitErr) {
			ambiguous = true
		}
		if !ambiguous && hasErrorLabel(commitErr, "TransientTransactionError") {
			return transactionAborted, true, commitErr
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
	return transactionPending, false, errors.Join(ErrCommitPending, lastErr)
}

func unknownCommitResult(err error) bool {
	return hasErrorLabel(err, "UnknownTransactionCommitResult") || mongo.IsTimeout(err) || errors.Is(err, context.Canceled) || mongo.IsNetworkError(err)
}

func hasErrorLabel(err error, label string) bool {
	var labeled interface{ HasErrorLabel(label string) bool }
	return errors.As(err, &labeled) && labeled.HasErrorLabel(label)
}
