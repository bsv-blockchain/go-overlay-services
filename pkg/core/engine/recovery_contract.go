package engine

// RecoveryLease identifies one worker's authority to publish or checkpoint a repair.
// Database time and this predicate must be used inside the same compare-and-swap as the update.
type RecoveryLease struct {
	HistoryFence
	Scope       StorageScope
	Topic       string
	PeerID      string
	JobID       string
	LeaseToken  StorageUint64
	ExpiresAtMS StorageUint64
}

// IsRecoveryLeaseCurrent reports whether current is still the exact expected unexpired lease at nowMS.
// Invalid uint64 values return an error so callers cannot treat malformed fencing data as current.
func IsRecoveryLeaseCurrent(expected, current RecoveryLease, nowMS StorageUint64) (bool, error) {
	if expected.Scope != current.Scope || expected.Topic != current.Topic || expected.PeerID != current.PeerID || expected.JobID != current.JobID {
		return false, nil
	}

	expectedChainEpoch, err := ParseStorageUint64(expected.ChainEpoch)
	if err != nil {
		return false, err
	}
	currentChainEpoch, err := ParseStorageUint64(current.ChainEpoch)
	if err != nil {
		return false, err
	}
	expectedGeneration, err := ParseStorageUint64(expected.TopicHistoryGeneration)
	if err != nil {
		return false, err
	}
	currentGeneration, err := ParseStorageUint64(current.TopicHistoryGeneration)
	if err != nil {
		return false, err
	}
	expectedToken, err := ParseStorageUint64(expected.LeaseToken)
	if err != nil {
		return false, err
	}
	currentToken, err := ParseStorageUint64(current.LeaseToken)
	if err != nil {
		return false, err
	}
	expiresAt, err := ParseStorageUint64(current.ExpiresAtMS)
	if err != nil {
		return false, err
	}
	now, err := ParseStorageUint64(nowMS)
	if err != nil {
		return false, err
	}

	return expectedChainEpoch == currentChainEpoch && expectedGeneration == currentGeneration && expectedToken == currentToken && expiresAt > now, nil
}

// HistoryRevisionHandoff records a recovery worker's checkpoint for a history update it caused.
// Other workers must rewind their dependent anchors and comparisons.
type HistoryRevisionHandoff struct {
	Expected   RecoveryLease
	Checkpoint string
}

// TopicAnchorRevision is immutable history whose current pointer must compare-and-swap both revisions and the header.
type TopicAnchorRevision struct {
	HistoryFence
	Scope         StorageScope
	Topic         string
	Height        StorageUint64
	BlockHash     string
	BASMRoot      string
	AdmittedCount StorageUint64
	TAC           string
	PreviousTAC   string
	PolicyID      string
}

// GASPCursorMode identifies evidence used to safely finalize a GASP cursor page.
type GASPCursorMode string

const (
	// GASPCursorModeNegotiatedTuple requires negotiated tuple semantics.
	GASPCursorModeNegotiatedTuple GASPCursorMode = "negotiated-tuple"
	// GASPCursorModeInclusiveSince requires inclusive-since semantics and equal-score draining.
	GASPCursorModeInclusiveSince GASPCursorMode = "inclusive-since"
	// GASPCursorModeFullResync requires completed no-skip full resynchronization.
	GASPCursorModeFullResync GASPCursorMode = "full-resync"
	// GASPCursorModeUnsupported cannot safely advance a cursor.
	GASPCursorModeUnsupported GASPCursorMode = "unsupported"
)

// GASPCursorEvidence records the evidence required to finalize one cursor page.
// Fields not used by Mode are ignored; the gate never advances a cursor itself.
type GASPCursorEvidence struct {
	Mode                     GASPCursorMode
	Negotiated               bool
	InclusiveSemanticsProven bool
	EqualScoreDrained        bool
	NoSkipSemanticsProven    bool
	ResyncCompleted          bool
	PageFinalized            bool
}

// CanAdvanceGASPCursor reports whether evidence permits a durable finalization and cursor transaction.
func CanAdvanceGASPCursor(evidence GASPCursorEvidence) bool {
	if !evidence.PageFinalized {
		return false
	}
	switch evidence.Mode {
	case GASPCursorModeNegotiatedTuple:
		return evidence.Negotiated
	case GASPCursorModeInclusiveSince:
		return evidence.InclusiveSemanticsProven && evidence.EqualScoreDrained
	case GASPCursorModeFullResync:
		return evidence.NoSkipSemanticsProven && evidence.ResyncCompleted
	default:
		return false
	}
}
