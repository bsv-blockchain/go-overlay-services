package engine_test

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/advertiser"
	"github.com/4chain-ag/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/universal-test-vectors/pkg/testabilities"
	"github.com/stretchr/testify/require"
)

type fakeStorage struct {
	findOutputFunc                  func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error)
	findOutputsFunc                 func(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*engine.Output, error)
	doesAppliedTransactionExistFunc func(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error)
	insertOutputFunc                func(ctx context.Context, utxo *engine.Output) error
	markUTXOsAsSpentFunc            func(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spendTxid *chainhash.Hash) error
	insertAppliedTransactionFunc    func(ctx context.Context, tx *overlay.AppliedTransaction) error
	updateConsumedByFunc            func(ctx context.Context, outpoint *transaction.Outpoint, topic string, consumedBy []*transaction.Outpoint) error
	deleteOutputFunc                func(ctx context.Context, outpoint *transaction.Outpoint, topic string) error
	findUTXOsForTopicFunc           func(ctx context.Context, topic string, since uint32, includeBEEF bool) ([]*engine.Output, error)
	updateTransactionBEEF           func(ctx context.Context, txid *chainhash.Hash, beef []byte) error
	updateOutputBlockHeight         func(ctx context.Context, outpoint *transaction.Outpoint, topic string, blockHeight uint32, blockIndex uint64, ancillaryBeef []byte) error
	findOutputsForTransaction       func(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error)
}

func (f fakeStorage) FindOutput(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
	if f.findOutputFunc != nil {
		return f.findOutputFunc(ctx, outpoint, topic, spent, includeBEEF)
	}
	panic("func not defined")
}
func (f fakeStorage) DoesAppliedTransactionExist(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error) {
	if f.doesAppliedTransactionExistFunc != nil {
		return f.doesAppliedTransactionExistFunc(ctx, tx)
	}
	panic("func not defined")
}
func (f fakeStorage) InsertOutput(ctx context.Context, utxo *engine.Output) error {
	if f.insertOutputFunc != nil {
		return f.insertOutputFunc(ctx, utxo)
	}
	panic("func not defined")
}

func (f fakeStorage) InsertAppliedTransaction(ctx context.Context, tx *overlay.AppliedTransaction) error {
	if f.insertAppliedTransactionFunc != nil {
		return f.insertAppliedTransactionFunc(ctx, tx)
	}
	panic("func not defined")
}
func (f fakeStorage) UpdateConsumedBy(ctx context.Context, outpoint *transaction.Outpoint, topic string, consumedBy []*transaction.Outpoint) error {
	if f.updateConsumedByFunc != nil {
		return f.updateConsumedByFunc(ctx, outpoint, topic, consumedBy)
	}
	panic("func not defined")
}
func (f fakeStorage) DeleteOutput(ctx context.Context, outpoint *transaction.Outpoint, topic string) error {
	if f.deleteOutputFunc != nil {
		return f.deleteOutputFunc(ctx, outpoint, topic)
	}
	panic("func not defined")
}
func (f fakeStorage) FindOutputs(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*engine.Output, error) {
	if f.findOutputsFunc != nil {
		return f.findOutputsFunc(ctx, outpoints, topic, spent, includeBEEF)
	}
	panic("func not defined")
}

func (f fakeStorage) FindOutputsForTransaction(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error) {
	if f.findOutputsForTransaction != nil {
		return f.findOutputsForTransaction(ctx, txid, includeBEEF)
	}
	panic("func not defined")
}

func (f fakeStorage) FindUTXOsForTopic(ctx context.Context, topic string, since uint32, includeBEEF bool) ([]*engine.Output, error) {
	if f.findUTXOsForTopicFunc != nil {
		return f.findUTXOsForTopicFunc(ctx, topic, since, includeBEEF)
	}
	panic("func not defined")
}

func (f fakeStorage) DeleteOutputs(ctx context.Context, outpoints []*transaction.Outpoint, topic string) error {
	if f.deleteOutputFunc != nil {
		return f.DeleteOutputs(ctx, outpoints, topic)
	}
	panic("func not defined")
}

func (f fakeStorage) MarkUTXOsAsSpent(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spendTxid *chainhash.Hash) error {
	if f.markUTXOsAsSpentFunc != nil {
		return f.markUTXOsAsSpentFunc(ctx, outpoints, topic, spendTxid)
	}
	panic("func not defined")
}

func (f fakeStorage) UpdateTransactionBEEF(ctx context.Context, txid *chainhash.Hash, beef []byte) error {
	if f.updateTransactionBEEF(ctx, txid, beef) != nil {
		return f.UpdateTransactionBEEF(ctx, txid, beef)
	}
	panic("func not defined")
}

func (f fakeStorage) UpdateOutputBlockHeight(ctx context.Context, outpoint *transaction.Outpoint, topic string, blockHeight uint32, blockIndex uint64, ancillaryBeef []byte) error {
	if f.updateOutputBlockHeight != nil {
		return f.updateOutputBlockHeight(ctx, outpoint, topic, blockHeight, blockIndex, ancillaryBeef)
	}
	panic("func not defined")
}

type fakeManager struct {
	identifyAdmissibleOutputsFunc func(ctx context.Context, beef []byte, previousCoins map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error)
	identifyNeededInputsFunc      func(ctx context.Context, beef []byte) ([]*transaction.Outpoint, error)
	getMetaData                   func() *overlay.MetaData
	getDocumentation              func() string
}

func (f fakeManager) IdentifyAdmissibleOutputs(ctx context.Context, beef []byte, previousCoins map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error) {
	if f.identifyAdmissibleOutputsFunc != nil {
		return f.identifyAdmissibleOutputsFunc(ctx, beef, previousCoins)
	}
	panic("func not defined")
}

func (f fakeManager) IdentifyNeededInputs(ctx context.Context, beef []byte) ([]*transaction.Outpoint, error) {
	if f.identifyNeededInputsFunc != nil {
		return f.identifyNeededInputsFunc(ctx, beef)
	}
	panic("func not defined")
}

func (f fakeManager) GetMetaData() *overlay.MetaData {
	if f.getMetaData != nil {
		return f.getMetaData()
	}
	panic("func not defined")
}

func (f fakeManager) GetDocumentation() string {
	if f.getDocumentation != nil {
		return f.getDocumentation()
	}
	panic("func not defined")
}

type fakeChainTracker struct {
	verifyFunc             func(tx *transaction.Transaction, options ...any) (bool, error)
	isValidRootForHeight   func(root *chainhash.Hash, height uint32) (bool, error)
	findHeaderFunc         func(height uint32) ([]byte, error)
	findPreviousHeaderFunc func(tx *transaction.Transaction) ([]byte, error)
}

func (f fakeChainTracker) Verify(tx *transaction.Transaction, options ...any) (bool, error) {
	if f.verifyFunc != nil {
		return f.verifyFunc(tx, options...)
	}
	panic("func not defined")
}

func (f fakeChainTracker) IsValidRootForHeight(root *chainhash.Hash, height uint32) (bool, error) {
	if f.isValidRootForHeight != nil {
		return f.isValidRootForHeight(root, height)
	}
	panic("func not defined")
}

func (f fakeChainTracker) FindHeader(height uint32) ([]byte, error) {
	if f.findHeaderFunc != nil {
		return f.findHeaderFunc(height)
	}
	panic("func not defined")
}

func (f fakeChainTracker) FindPreviousHeader(tx *transaction.Transaction) ([]byte, error) {
	if f.findPreviousHeaderFunc != nil {
		return f.findPreviousHeaderFunc(tx)
	}
	panic("func not defined")
}

type fakeChainTrackerSPVFail struct{}

func (f fakeChainTrackerSPVFail) Verify(tx *transaction.Transaction, options ...any) (bool, error) {
	panic("func not defined")
}

func (f fakeChainTrackerSPVFail) IsValidRootForHeight(root *chainhash.Hash, height uint32) (bool, error) {
	panic("func not defined")
}

func (f fakeChainTrackerSPVFail) FindHeader(height uint32) ([]byte, error) {
	panic("func not defined")
}

func (f fakeChainTrackerSPVFail) FindPreviousHeader(tx *transaction.Transaction) ([]byte, error) {
	panic("func not defined")
}

type fakeBroadcasterFail struct {
	broadcastFunc    func(tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure)
	broadcastCtxFunc func(ctx context.Context, tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure)
}

func (f fakeBroadcasterFail) Broadcast(tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	if f.broadcastFunc != nil {
		return f.broadcastFunc(tx)
	}
	panic("func not defined")
}

func (f fakeBroadcasterFail) BroadcastCtx(ctx context.Context, tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
	if f.broadcastCtxFunc != nil {
		return f.broadcastCtxFunc(ctx, tx)
	}
	panic("func not defined")
}

type fakeLookupService struct {
	lookupFunc func(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error)
}

func (f fakeLookupService) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	if f.lookupFunc != nil {
		return f.lookupFunc(ctx, question)
	}
	panic("func not defined")
}

func (f fakeLookupService) OutputAdmittedByTopic(ctx context.Context, payload *engine.OutputAdmittedByTopic) error {
	panic("func not defined")
}

func (f fakeLookupService) OutputSpent(ctx context.Context, payload *engine.OutputSpent) error {
	panic("func not defined")
}

func (f fakeLookupService) OutputNoLongerRetainedInHistory(ctx context.Context, outpoint *transaction.Outpoint, topic string) error {
	panic("func not defined")
}

func (f fakeLookupService) OutputEvicted(ctx context.Context, outpoint *transaction.Outpoint) error {
	panic("func not defined")
}

func (f fakeLookupService) OutputBlockHeightUpdated(ctx context.Context, txid *chainhash.Hash, blockHeight uint32, blockIndex uint64) error {
	panic("func not defined")
}

func (f fakeLookupService) GetDocumentation() string {
	panic("func not defined")
}

func (f fakeLookupService) GetMetaData() *overlay.MetaData {
	return &overlay.MetaData{}
}

type fakeAdvertiser struct {
	findAllAdvertisements     func(protocol overlay.Protocol) ([]*advertiser.Advertisement, error)
	createAdvertisements      func(data []*advertiser.AdvertisementData) (overlay.TaggedBEEF, error)
	revokeAdvertisements      func(data []*advertiser.Advertisement) (overlay.TaggedBEEF, error)
	parseAdvertisement        func(script *script.Script) (*advertiser.Advertisement, error)
	findAllAdvertisementsFunc func(protocol overlay.Protocol) ([]*advertiser.Advertisement, error)
	createAdvertisementsFunc  func(data []*advertiser.AdvertisementData) (overlay.TaggedBEEF, error)
	revokeAdvertisementsFunc  func(data []*advertiser.Advertisement) (overlay.TaggedBEEF, error)
}

func (f fakeAdvertiser) FindAllAdvertisements(protocol overlay.Protocol) ([]*advertiser.Advertisement, error) {
	if f.findAllAdvertisements != nil {
		return f.findAllAdvertisements(protocol)
	}
	return nil, nil
}
func (f fakeAdvertiser) CreateAdvertisements(data []*advertiser.AdvertisementData) (overlay.TaggedBEEF, error) {
	if f.createAdvertisements != nil {
		return f.createAdvertisements(data)
	}
	return overlay.TaggedBEEF{}, nil
}
func (f fakeAdvertiser) RevokeAdvertisements(data []*advertiser.Advertisement) (overlay.TaggedBEEF, error) {
	if f.revokeAdvertisements != nil {
		return f.revokeAdvertisements(data)
	}
	return overlay.TaggedBEEF{}, nil
}
func (f fakeAdvertiser) ParseAdvertisement(script *script.Script) (*advertiser.Advertisement, error) {
	if f.parseAdvertisement != nil {
		return f.parseAdvertisement(script)
	}
	return nil, nil
}

type fakeTopicManager struct{}

func (fakeTopicManager) IdentifyAdmissibleOutputs(ctx context.Context, beef []byte, previousCoins map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error) {
	return overlay.AdmittanceInstructions{}, nil
}
func (fakeTopicManager) IdentifyNeededInputs(ctx context.Context, beef []byte) ([]*transaction.Outpoint, error) {
	return nil, nil
}
func (fakeTopicManager) GetMetaData() *overlay.MetaData {
	return &overlay.MetaData{}
}
func (fakeTopicManager) GetDocumentation() string {
	return ""
}

// helper function to create a dummy BEEF transaction
// This function creates a dummy BEEF transaction with a single output and no inputs.
// It returns the serialized bytes of the BEEF transaction.
// The transaction is created with a dummy locking script that contains an OP_RETURN opcode.
func createDummyBEEF(t *testing.T) []byte {
	t.Helper()

	dummyTx := testabilities.GivenTX().
		WithInput(1000).
		WithP2PKHOutput(999).
		TX()

	BEEF, err := transaction.NewBeefFromTransaction(dummyTx)
	require.NoError(t, err)

	bytes, err := BEEF.AtomicBytes(dummyTx.TxID())
	require.NoError(t, err)
	return bytes
}

func parseBEEFToTx(t *testing.T, bytes []byte) *transaction.Transaction {
	t.Helper()

	_, tx, _, err := transaction.ParseBeef(bytes)
	require.NoError(t, err)
	return tx
}

// createDummyValidTaggedBEEF creates a dummy valid tagged BEEF transaction for testing.
// It creates a previous transaction and a current transaction, both with dummy locking scripts.
// The previous transaction is used as an input for the current transaction.
// It returns the tagged BEEF and the transaction ID of the previous transaction.
// The tagged BEEF contains a list of topics and the serialized bytes of the BEEF transaction.
func createDummyValidTaggedBEEF(t *testing.T) (overlay.TaggedBEEF, *chainhash.Hash) {
	t.Helper()
	prevTx := &transaction.Transaction{
		Inputs:  []*transaction.TransactionInput{},
		Outputs: []*transaction.TransactionOutput{{Satoshis: 1000, LockingScript: &script.Script{script.OpTRUE}}},
	}
	prevTxID := prevTx.TxID()

	currentTx := &transaction.Transaction{
		Inputs:  []*transaction.TransactionInput{{SourceTXID: prevTxID, SourceTxOutIndex: 0}},
		Outputs: []*transaction.TransactionOutput{{Satoshis: 900, LockingScript: &script.Script{script.OpTRUE}}},
	}
	currentTxID := currentTx.TxID()

	beef := &transaction.Beef{
		Version: transaction.BEEF_V2,
		Transactions: map[string]*transaction.BeefTx{
			prevTxID.String():    {Transaction: prevTx},
			currentTxID.String(): {Transaction: currentTx},
		},
	}
	beefBytes, err := beef.AtomicBytes(currentTxID)
	require.NoError(t, err)

	return overlay.TaggedBEEF{Topics: []string{"test-topic"}, Beef: beefBytes}, prevTxID
}

// fakeTxID returns a fixed valid chainhash.Hash for testing purposes.
func fakeTxID(t *testing.T) chainhash.Hash {
	t.Helper()

	const hexStr = "03895fb984362a4196bc9931629318fcbb2aeba7c6293638119ea653fa31d119"
	b, err := hex.DecodeString(hexStr)
	require.NoError(t, err)

	var h chainhash.Hash
	copy(h[:], b)
	return h
}

// createDummyBeefWithInputs creates a dummy BEEF transaction with inputs for testing.
// It creates a previous transaction with a dummy locking script and a current transaction
// that uses the previous transaction as an input. The current transaction also has a dummy locking script.
// It returns the serialized bytes of the BEEF transaction.
func createDummyBeefWithInputs(t *testing.T) []byte {
	t.Helper()

	prevTxID := chainhash.DoubleHashH([]byte("dummy prev tx"))

	dummyLockingScript := script.Script{script.OpTRUE}

	prevTx := &transaction.Transaction{
		Inputs:  []*transaction.TransactionInput{},
		Outputs: []*transaction.TransactionOutput{{Satoshis: 1000, LockingScript: &dummyLockingScript}},
	}

	currentTx := &transaction.Transaction{
		Inputs: []*transaction.TransactionInput{
			{SourceTXID: &prevTxID, SourceTxOutIndex: 0},
		},
		Outputs: []*transaction.TransactionOutput{
			{Satoshis: 900, LockingScript: &dummyLockingScript},
		},
	}

	beef := &transaction.Beef{
		Version: transaction.BEEF_V2,
		Transactions: map[string]*transaction.BeefTx{
			prevTx.TxID().String():    {Transaction: prevTx},
			currentTx.TxID().String(): {Transaction: currentTx},
		},
	}

	beefBytes, err := beef.AtomicBytes(currentTx.TxID())
	require.NoError(t, err)

	return beefBytes
}
