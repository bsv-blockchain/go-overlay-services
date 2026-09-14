package identity

import (
	"context"
	"fmt"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// TopicManager implements identity application admission independently per output.
// It is safe for concurrent calls when callers do not mutate supplied BEEF graphs.
type TopicManager struct {
	policy AdmissionPolicy
}

var _ engine.TopicManager = (*TopicManager)(nil)

// NewTopicManager creates an opt-in manager with DefaultAdmissionPolicy limits.
func NewTopicManager() *TopicManager {
	return &TopicManager{policy: DefaultAdmissionPolicy()}
}

// NewTopicManagerWithPolicy creates a manager with explicit application budgets.
// policy must have positive limits; it does not configure engine verification.
func NewTopicManagerWithPolicy(policy AdmissionPolicy) (*TopicManager, error) {
	if err := policy.validate(); err != nil {
		return nil, err
	}
	return &TopicManager{policy: policy}, nil
}

// IdentifyAdmissibleOutputs validates the transaction selected by txid in beef.
// ctx cancels work. previousCoins does not affect identity retention: coins to
// retain are always empty. Invalid outputs are skipped independently; malformed
// transaction selection or exhausted transaction-wide budgets return an error.
// Successful application admission does not establish transaction/SPV validity.
func (m *TopicManager) IdentifyAdmissibleOutputs(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, _ []uint32) (overlay.AdmittanceInstructions, error) {
	result := overlay.AdmittanceInstructions{OutputsToAdmit: []uint32{}, CoinsToRetain: []uint32{}}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	tx, err := selectedTransaction(beef, txid, m.policy)
	if err != nil {
		return result, err
	}
	if len(tx.Inputs) == 0 || len(tx.Outputs) == 0 {
		return result, nil
	}
	for index, output := range tx.Outputs {
		if err = ctx.Err(); err != nil {
			return overlay.AdmittanceInstructions{}, err
		}
		// selectedTransaction bounds output count, which is limited to uint32
		// by serialized transaction outpoints.
		outpoint := transaction.Outpoint{Txid: *txid, Index: uint32(index)}
		if _, outputErr := ProjectOutput(ctx, outpoint, output.LockingScript, m.policy); outputErr == nil {
			result.OutputsToAdmit = append(result.OutputsToAdmit, outpoint.Index)
		}
	}
	if err = ctx.Err(); err != nil {
		return overlay.AdmittanceInstructions{}, err
	}
	return result, nil
}

func selectedTransaction(beef *transaction.Beef, txid *chainhash.Hash, policy AdmissionPolicy) (*transaction.Transaction, error) {
	tx, err := lookupSelectedTransaction(beef, txid)
	if err != nil {
		return nil, err
	}
	if err = boundSelectedOutputs(tx, policy); err != nil {
		return nil, err
	}
	if err = requireSelectedInputs(tx); err != nil {
		return nil, err
	}
	// The BEEF map is caller-supplied. Bind its selected ID to serialized bytes
	// instead of trusting a map key (without claiming graph/SPV verification).
	if !tx.TxID().Equal(*txid) {
		return nil, fmt.Errorf("%w: selected txid does not match bytes", ErrInvalidTransaction)
	}
	return tx, nil
}

func lookupSelectedTransaction(beef *transaction.Beef, txid *chainhash.Hash) (*transaction.Transaction, error) {
	if beef == nil || txid == nil {
		return nil, ErrInvalidTransaction
	}
	entry := beef.Transactions[*txid]
	if entry == nil || entry.Transaction == nil || entry.DataFormat == transaction.TxIDOnly {
		return nil, ErrInvalidTransaction
	}
	return entry.Transaction, nil
}

func boundSelectedOutputs(tx *transaction.Transaction, policy AdmissionPolicy) error {
	if len(tx.Outputs) > policy.MaxOutputs || uint64(len(tx.Outputs)) > 1<<32 {
		return ErrAdmissionBudget
	}
	total := 0
	for _, output := range tx.Outputs {
		if output == nil || output.LockingScript == nil {
			return ErrInvalidTransaction
		}
		if len(*output.LockingScript) > policy.MaxTotalScriptBytes-total {
			return ErrAdmissionBudget
		}
		total += len(*output.LockingScript)
	}
	return nil
}

func requireSelectedInputs(tx *transaction.Transaction) error {
	for _, input := range tx.Inputs {
		if input == nil || input.SourceTXID == nil || input.UnlockingScript == nil {
			return ErrInvalidTransaction
		}
	}
	return nil
}

// IdentifyNeededInputs requests no inputs beyond the engine's verification
// requirements. ctx is honored; beef and txid do not add identity dependencies.
func (m *TopicManager) IdentifyNeededInputs(ctx context.Context, _ *transaction.Beef, _ *chainhash.Hash) ([]*transaction.Outpoint, error) {
	return []*transaction.Outpoint{}, ctx.Err()
}

// GetDocumentation describes the manager's application boundary.
func (m *TopicManager) GetDocumentation() string {
	return "Identity Topic Manager: register verifiable identity certificates for public discovery. Engine transaction/SPV verification is separately required."
}

// GetMetaData returns a new metadata value for this manager.
func (m *TopicManager) GetMetaData() *overlay.MetaData {
	return &overlay.MetaData{Name: "Identity Topic Manager", Description: "Identity Resolution Protocol"}
}
