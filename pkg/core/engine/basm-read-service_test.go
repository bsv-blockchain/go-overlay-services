package engine

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
)

const genesisRawHex = "01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff4d04ffff001d0104455468652054696d65732030332f4a616e2f32303039204368616e63656c6c6f72206f6e206272696e6b206f66207365636f6e64206261696c6f757420666f722062616e6b73ffffffff0100f2052a01000000434104678afdb0fe5548271967f1a67130b7105cd6a828e03909a67962e0ea1f61deb649f6bc3f4cef38c4f35504e51ec112de5c384df7ba0b8d578a4c702b6bf11d5fac00000000"

var errFakeClose = errors.New("fake view close failed")

func TestBASMReadServiceServesAllMethodsAgainstImmutableFixture(t *testing.T) {
	fx := newReadFixture(t)
	service := newReadService(t, fx.storage(), fx.headers, fx.limits)

	tip, err := service.ProvideTopicAnchorTip(context.Background(), fx.topic)
	require.NoError(t, err)
	assert.Equal(t, int64(3), tip.BlockHeight)
	require.Equal(t, fx.anchors[3].TAC, tip.TAC)

	rangeResult, err := service.ProvideTopicAnchorRange(context.Background(), fx.topic, 0, 3)
	require.NoError(t, err)
	require.Len(t, rangeResult.Anchors, 4)
	assert.Equal(t, uint64(0), rangeResult.Anchors[3].AdmittedCount, "empty height remains in the contiguous range")

	list, err := service.ProvideAdmittedList(context.Background(), fx.topic, 2, nil)
	require.NoError(t, err)
	require.Equal(t, fx.leaves[1], list.Admitted[0].TxID)
	require.Equal(t, uint64(1), list.Admitted[0].BlockIndex)
	orphan := fx.anchors[2].BlockHash
	orphan[0] ^= 1
	_, err = service.ProvideAdmittedList(context.Background(), fx.topic, 2, &orphan)
	require.ErrorIs(t, err, ErrBASMNotReady)

	requested := []basm.Hash{fx.leaves[1], fx.leaves[2]}
	wantRequested := append([]basm.Hash(nil), requested...)
	proof, err := service.ProvideCompoundMerklePath(context.Background(), fx.topic, 2, requested)
	require.NoError(t, err)
	assert.Equal(t, wantRequested, requested, "serving must not mutate caller-owned requested IDs")
	assert.Equal(t, wantRequested, proof.TxIDs)
	assert.NotEmpty(t, proof.MerklePath)
	pruned, err := service.ProvideCompoundMerklePath(context.Background(), fx.topic, 1, []basm.Hash{fx.leaves[0]})
	require.NoError(t, err, "a complete base layer can derive omitted internal proof levels")
	assert.NotEmpty(t, pruned.MerklePath)

	raw, err := service.ProvideRawTransactions(context.Background(), []basm.Hash{fx.genesisTXID, fx.leaves[4]})
	require.NoError(t, err)
	require.Len(t, raw.Transactions, 1)
	assert.Equal(t, fx.genesisTXID, raw.Transactions[0].TxID)
	assert.Equal(t, []basm.Hash{fx.leaves[4]}, raw.Missing)
	assert.GreaterOrEqual(t, fx.view.closed, 5)
}

func TestBASMReadServiceCapabilityAndReadinessBoundaries(t *testing.T) {
	fx := newReadFixture(t)
	_, err := NewBASMReadService(nil, fx.headers, fx.limits)
	require.ErrorIs(t, err, ErrBASMUnsupported)

	service := newReadService(t, fx.storage(), nil, fx.limits)
	_, err = service.ProvideTopicAnchorTip(context.Background(), fx.topic)
	require.ErrorIs(t, err, ErrBASMNotReady)
	_, err = service.ProvideTopicAnchorRange(context.Background(), fx.topic, 0, 0)
	require.ErrorIs(t, err, ErrBASMNotReady)
	_, err = service.ProvideAdmittedList(context.Background(), fx.topic, 0, nil)
	require.ErrorIs(t, err, ErrBASMNotReady)
	_, err = service.ProvideCompoundMerklePath(context.Background(), fx.topic, 0, []basm.Hash{fx.genesisTXID})
	require.ErrorIs(t, err, ErrBASMNotReady)
	_, err = service.ProvideRawTransactions(context.Background(), []basm.Hash{fx.genesisTXID})
	require.NoError(t, err, "raw storage reads do not need a canonical header resolver")

	fx.view.tip = nil
	ready := newReadService(t, fx.storage(), fx.headers, fx.limits)
	tip, err := ready.ProvideTopicAnchorTip(context.Background(), fx.topic)
	require.NoError(t, err)
	assert.Equal(t, basm.TopicAnchorTip{Topic: fx.topic, BlockHeight: -1}, tip)
}

func TestBASMReadServiceRejectsTypedNilCapabilities(t *testing.T) {
	fx := newReadFixture(t)
	var storage *fakeBASMReadStorage
	_, err := NewBASMReadService(storage, fx.headers, fx.limits)
	require.ErrorIs(t, err, ErrBASMUnsupported)
	var storageFunction basmStorageFunction
	_, err = NewBASMReadService(storageFunction, fx.headers, fx.limits)
	require.ErrorIs(t, err, ErrBASMUnsupported)

	var headers *fakeBASMHeaders
	service := newReadService(t, fx.storage(), headers, fx.limits)
	_, err = service.ProvideTopicAnchorTip(context.Background(), fx.topic)
	require.ErrorIs(t, err, ErrBASMNotReady)
	_, err = service.ProvideRawTransactions(context.Background(), []basm.Hash{fx.genesisTXID})
	require.NoError(t, err, "typed-nil headers keep raw-only service available")

	service = newReadService(t, &fakeBASMReadStorage{opens: &fx.storageOpens}, fx.headers, fx.limits)
	_, err = service.ProvideTopicAnchorTip(context.Background(), fx.topic)
	require.ErrorIs(t, err, ErrBASMNotReady, "typed-nil returned views are never read or closed")
	_, err = service.ProvideRawTransactions(context.Background(), nil)
	require.ErrorIs(t, err, ErrBASMNotReady)
}

func TestBASMReadServiceNilReceiverIsUnsupported(t *testing.T) {
	var service *BASMReadService
	assert.False(t, IsBASMProviderAvailable(service))
	_, err := service.ProvideTopicAnchorTip(context.Background(), "topic")
	require.ErrorIs(t, err, ErrBASMUnsupported)
	_, err = service.ProvideTopicAnchorRange(context.Background(), "topic", 0, 0)
	require.ErrorIs(t, err, ErrBASMUnsupported)
	_, err = service.ProvideAdmittedList(context.Background(), "topic", 0, nil)
	require.ErrorIs(t, err, ErrBASMUnsupported)
	_, err = service.ProvideCompoundMerklePath(context.Background(), "topic", 0, nil)
	require.ErrorIs(t, err, ErrBASMUnsupported)
	_, err = service.ProvideRawTransactions(context.Background(), nil)
	require.ErrorIs(t, err, ErrBASMUnsupported)
}

type basmStorageFunction func(context.Context, string, basm.ReadLimits) (BASMReadView, error)

func (f basmStorageFunction) OpenBASMRead(ctx context.Context, topic string, limits basm.ReadLimits) (BASMReadView, error) {
	return f(ctx, topic, limits)
}

func TestBASMReadServiceRejectsInconsistentAnchorsAndLists(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*readFixture)
		run    func(*BASMReadService, *readFixture) error
	}{
		{
			name:   "range gap",
			mutate: func(fx *readFixture) { fx.view.anchorOverride = []basm.Anchor{fx.anchors[0], fx.anchors[2]} },
			run: func(s *BASMReadService, fx *readFixture) error {
				_, err := s.ProvideTopicAnchorRange(context.Background(), fx.topic, 0, 1)
				return err
			},
		},
		{
			name:   "reordered range",
			mutate: func(fx *readFixture) { fx.view.anchorOverride = []basm.Anchor{fx.anchors[1], fx.anchors[0]} },
			run: func(s *BASMReadService, fx *readFixture) error {
				_, err := s.ProvideTopicAnchorRange(context.Background(), fx.topic, 0, 1)
				return err
			},
		},
		{
			name: "tac mismatch",
			mutate: func(fx *readFixture) {
				a := fx.anchors[2]
				a.TAC[0] ^= 1
				fx.view.anchorOverride = []basm.Anchor{fx.anchors[1], a}
			},
			run: func(s *BASMReadService, fx *readFixture) error {
				_, err := s.ProvideTopicAnchorRange(context.Background(), fx.topic, 1, 2)
				return err
			},
		},
		{
			name: "anchor topic mismatch",
			mutate: func(fx *readFixture) {
				a := fx.anchors[2]
				a.Topic = "other"
				fx.view.anchorOverride = []basm.Anchor{a}
			},
			run: func(s *BASMReadService, fx *readFixture) error {
				_, err := s.ProvideAdmittedList(context.Background(), fx.topic, 2, nil)
				return err
			},
		},
		{
			name: "duplicate admitted txid",
			mutate: func(fx *readFixture) {
				fx.view.admitted[2] = []basm.AdmittedTxRef{{TxID: fx.leaves[1], BlockIndex: 1}, {TxID: fx.leaves[1], BlockIndex: 2}}
			},
			run: func(s *BASMReadService, fx *readFixture) error {
				_, err := s.ProvideAdmittedList(context.Background(), fx.topic, 2, nil)
				return err
			},
		},
		{
			name:   "out of range original position",
			mutate: func(fx *readFixture) { fx.view.admitted[2] = []basm.AdmittedTxRef{{TxID: fx.leaves[1], BlockIndex: 3}} },
			run: func(s *BASMReadService, fx *readFixture) error {
				_, err := s.ProvideAdmittedList(context.Background(), fx.topic, 2, nil)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fx := newReadFixture(t)
			test.mutate(fx)
			err := test.run(newReadService(t, fx.storage(), fx.headers, fx.limits), fx)
			require.Error(t, err)
		})
	}
}

func TestBASMReadServiceRejectsAnchorCountAboveCanonicalBlockCount(t *testing.T) {
	tests := []struct {
		name string
		run  func(*BASMReadService, *readFixture) error
	}{
		{
			name: "tip",
			run: func(service *BASMReadService, fixture *readFixture) error {
				anchor := fixture.anchors[1]
				anchor.AdmittedCount = 5 // the independent header at height 1 has four transactions
				fixture.view.tip = &anchor
				response, err := service.ProvideTopicAnchorTip(context.Background(), fixture.topic)
				assert.Equal(t, basm.TopicAnchorTip{}, response)
				return err
			},
		},
		{
			name: "range",
			run: func(service *BASMReadService, fixture *readFixture) error {
				anchor := fixture.anchors[1]
				anchor.AdmittedCount = 5 // the independent header at height 1 has four transactions
				fixture.view.anchorOverride = []basm.Anchor{anchor}
				response, err := service.ProvideTopicAnchorRange(context.Background(), fixture.topic, 1, 1)
				assert.Equal(t, basm.TopicAnchorRange{}, response)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReadFixture(t)
			err := test.run(newReadService(t, fixture.storage(), fixture.headers, fixture.limits), fixture)
			require.ErrorIs(t, err, ErrBASMInvalidData)
			require.Equal(t, 1, fixture.view.closed)
		})
	}
}

func TestBASMReadServiceRejectsReorgAndHistoryChangesBeforeReturning(t *testing.T) {
	fx := newReadFixture(t)
	changed := fx.headers.headers[2]
	changed.BlockHash[0] ^= 1
	fx.headers.changed[2] = changed
	fx.headers.changeOnSecond[2] = true
	service := newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err := service.ProvideAdmittedList(context.Background(), fx.topic, 2, nil)
	require.ErrorIs(t, err, ErrBASMNotReady)

	fx = newReadFixture(t)
	fx.view.checkErr = ErrBASMNotReady
	service = newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err = service.ProvideTopicAnchorTip(context.Background(), fx.topic)
	require.ErrorIs(t, err, ErrBASMNotReady)
	require.Equal(t, 1, fx.view.closed)
}

func TestBASMReadServiceRejectsMalformedRawAndHonorsBoundsBeforeReads(t *testing.T) {
	fx := newReadFixture(t)
	service := newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err := service.ProvideRawTransactions(context.Background(), []basm.Hash{fx.genesisTXID, fx.genesisTXID})
	require.ErrorIs(t, err, basm.ErrInvalidInput)
	assert.Equal(t, 0, fx.storageOpens)

	fx = newReadFixture(t)
	fx.limits.MaxRange = 1
	service = newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err = service.ProvideTopicAnchorRange(context.Background(), fx.topic, 0, 1)
	require.ErrorIs(t, err, basm.ErrLimitExceeded)
	assert.Equal(t, 0, fx.storageOpens)

	tooMany := []basm.Hash{fx.leaves[0], fx.leaves[1], fx.leaves[2]}
	fx.limits.MaxRequestedTxIDs = 2
	service = newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err = service.ProvideRawTransactions(context.Background(), tooMany)
	require.ErrorIs(t, err, basm.ErrLimitExceeded)
	assert.Equal(t, 0, fx.storageOpens)

	fx = newReadFixture(t)
	fx.view.raw[fx.genesisTXID] = []byte{1, 0, 0, 0, 0, 0, 0, 0, 0xef, 0, 0, 0, 0}
	service = newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err = service.ProvideRawTransactions(context.Background(), []basm.Hash{fx.genesisTXID})
	require.ErrorIs(t, err, ErrBASMInvalidData)

	fx = newReadFixture(t)
	wrong := fx.genesisTXID
	wrong[0] ^= 1
	fx.view.raw[wrong] = fx.rawGenesis
	service = newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err = service.ProvideRawTransactions(context.Background(), []basm.Hash{wrong})
	require.ErrorIs(t, err, ErrBASMInvalidData)

	fx = newReadFixture(t)
	fx.limits.MaxResponseBytes = 100
	service = newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err = service.ProvideRawTransactions(context.Background(), []basm.Hash{fx.genesisTXID})
	require.ErrorIs(t, err, basm.ErrLimitExceeded)
}

func TestBASMReadServiceClosesViewsForCloseErrorsAndCancellation(t *testing.T) {
	fx := newReadFixture(t)
	fx.view.closeErr = errFakeClose
	service := newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err := service.ProvideTopicAnchorTip(context.Background(), fx.topic)
	require.ErrorIs(t, err, fx.view.closeErr)
	require.Equal(t, 1, fx.view.closed)

	fx = newReadFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	fx.view.cancel = cancel
	service = newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err = service.ProvideTopicAnchorTip(ctx, fx.topic)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, fx.view.closed)
}

func TestBASMReadServiceBoundsEveryResponseShape(t *testing.T) {
	fx := newReadFixture(t)
	fx.limits.MaxResponseBytes = 1
	service := newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err := service.ProvideTopicAnchorTip(context.Background(), fx.topic)
	require.ErrorIs(t, err, basm.ErrLimitExceeded)
	_, err = service.ProvideTopicAnchorRange(context.Background(), fx.topic, 0, 1)
	require.ErrorIs(t, err, basm.ErrLimitExceeded)
	_, err = service.ProvideAdmittedList(context.Background(), fx.topic, 2, nil)
	require.ErrorIs(t, err, basm.ErrLimitExceeded)
	_, err = service.ProvideCompoundMerklePath(context.Background(), fx.topic, 2, []basm.Hash{fx.leaves[1]})
	require.ErrorIs(t, err, basm.ErrLimitExceeded)
	_, err = service.ProvideRawTransactions(context.Background(), nil)
	require.ErrorIs(t, err, basm.ErrLimitExceeded)
	require.ErrorIs(t, service.checkResponseSize(0, ^uint64(0), ^uint64(0)), basm.ErrLimitExceeded)
}

func TestBASMReadServiceServesRawTransactionAbove16MiB(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a 17 MiB raw transaction and its hex response")
	}
	fx := newReadFixture(t)
	tx, err := transaction.NewTransactionFromBytes(fx.rawGenesis)
	require.NoError(t, err)
	tx.Outputs[0].LockingScript = script.NewFromBytes(bytes.Repeat([]byte{0x51}, 17<<20))
	raw := tx.Bytes()
	txid := basm.Hash(*tx.TxID())
	fx.view.raw[txid] = raw
	service := newReadService(t, fx.storage(), fx.headers, fx.limits)
	response, err := service.ProvideRawTransactions(context.Background(), []basm.Hash{txid})
	require.NoError(t, err)
	require.Len(t, response.Transactions, 1)
	assert.Greater(t, len(raw), 16<<20)
	assert.Len(t, response.Transactions[0].RawTx, len(raw)*2)
	assert.Equal(t, txid, response.Transactions[0].TxID)
}

func TestBASMReadServiceRejectsInvalidProofsAndClosesViews(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*readFixture)
		errIs  error
	}{
		{name: "non admitted requested txid", mutate: func(fx *readFixture) { fx.view.proofs[fx.leaves[0]] = fx.proofB }, errIs: ErrBASMNotFound},
		{name: "proof root mismatch", mutate: func(fx *readFixture) {
			header := fx.headers.headers[2]
			header.MerkleRoot[0] ^= 1
			fx.headers.headers[2] = header
		}, errIs: ErrBASMInvalidData},
		{name: "missing sibling", mutate: func(fx *readFixture) {
			fx.view.proofs[fx.leaves[1]] = bumpBytes(2, [][]proofNode{{{offset: 1, hash: fx.leaves[1], txid: true}}})
		}, errIs: ErrBASMInvalidData},
		{name: "wrong proof height", mutate: func(fx *readFixture) {
			fx.view.proofs[fx.leaves[1]] = bumpBytes(3, [][]proofNode{{{offset: 0, hash: fx.leaves[0]}, {offset: 1, hash: fx.leaves[1], txid: true}}, {{offset: 1, hash: mustHash(t, "df2db416a05165238668a5d299b773f5f1bd00364b37c9d8b3d299367e4b3d64")}}})
		}, errIs: ErrBASMInvalidData},
		{name: "wrong original block position", mutate: func(fx *readFixture) {
			fx.view.proofs[fx.leaves[1]] = bumpBytes(2, [][]proofNode{{{offset: 0, hash: fx.leaves[1], txid: true}, {offset: 1, hash: fx.leaves[0]}}, {{offset: 1, hash: mustHash(t, "df2db416a05165238668a5d299b773f5f1bd00364b37c9d8b3d299367e4b3d64")}}})
		}, errIs: ErrBASMInvalidData},
		{name: "deep sparse path", mutate: func(fx *readFixture) { fx.view.proofs[fx.leaves[1]] = bumpBytes(2, make([][]proofNode, 3)) }, errIs: ErrBASMInvalidData},
		{name: "aggregate proof cap", mutate: func(fx *readFixture) { fx.limits.MaxProofBytes = proofCap(fx.proofB, fx.proofC) }, errIs: basm.ErrLimitExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fx := newReadFixture(t)
			test.mutate(fx)
			service := newReadService(t, fx.storage(), fx.headers, fx.limits)
			ids := []basm.Hash{fx.leaves[1]}
			if test.name == "non admitted requested txid" {
				ids = []basm.Hash{fx.leaves[0]}
			}
			if test.name == "aggregate proof cap" {
				ids = append(ids, fx.leaves[2])
			}
			_, err := service.ProvideCompoundMerklePath(context.Background(), fx.topic, 2, ids)
			require.ErrorIs(t, err, test.errIs)
			require.Equal(t, 1, fx.view.closed)
		})
	}
}

type readFixture struct {
	topic        string
	leaves       []basm.Hash
	genesisTXID  basm.Hash
	rawGenesis   []byte
	anchors      map[uint32]basm.Anchor
	proofB       []byte
	proofC       []byte
	view         *fakeBASMReadView
	headers      *fakeBASMHeaders
	limits       basm.ReadLimits
	storageOpens int
}

func newReadFixture(t *testing.T) *readFixture {
	t.Helper()
	genesisID := mustHash(t, "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b")
	genesisBlock := mustHash(t, "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f")
	// The hashes below are synthetic local leaves. Their 2/3/5-leaf roots were
	// independently calculated with Python SHA256d before this test was written.
	leaves := []basm.Hash{repeatHash(1), repeatHash(2), repeatHash(3), repeatHash(4), repeatHash(5)}
	headers := &fakeBASMHeaders{headers: map[uint32]BASMCanonicalHeader{
		0: {Height: 0, BlockHash: genesisBlock, MerkleRoot: genesisID, TransactionCount: 1},
		1: {Height: 1, BlockHash: repeatHash(11), MerkleRoot: mustHash(t, "12c8e8d4975689696b32029894a9266711df6b98a1c9871b706886f9aeab5a08"), TransactionCount: 4},
		2: {Height: 2, BlockHash: repeatHash(12), MerkleRoot: mustHash(t, "acbd47d5022a6c5e954ad677df8ec221c893f871889826df53f0f1ad3f023e22"), TransactionCount: 3},
		3: {Height: 3, BlockHash: repeatHash(13), MerkleRoot: mustHash(t, "d3e522600f2e93120ad9331f4e199c5dd98262a23ff8ae8b3f8b36720f87e226"), TransactionCount: 5},
	}, changed: map[uint32]BASMCanonicalHeader{}, changeOnSecond: map[uint32]bool{}, calls: map[uint32]int{}}
	fx := &readFixture{topic: "tm_fixture", leaves: leaves, genesisTXID: genesisID, headers: headers, limits: basm.DefaultReadLimits()}
	fx.limits.MaxAdmitted = 10
	fx.limits.MaxRange = 10
	raw, err := hex.DecodeString(genesisRawHex)
	require.NoError(t, err)
	fx.rawGenesis = raw
	anchors := map[uint32]basm.Anchor{}
	previous := basm.Hash{}
	for height := uint32(0); height < 4; height++ {
		root := basm.Hash{}
		count := uint64(0)
		if height == 0 {
			root, count = genesisID, 1
		}
		if height == 1 {
			root, count = leaves[0], 1
		}
		if height == 2 {
			root, count = mustHash(t, "1b595fc4fcbf0af71c4cdda29dc42e88211fff2aa54acfcce6ab6fca42c1121b"), 2
		}
		a := basm.Anchor{TopicBlockAnchor: basm.TopicBlockAnchor{Topic: fx.topic, BlockHeight: height, BlockHash: headers.headers[height].BlockHash, BASMRoot: root, AdmittedCount: count}}
		a.TAC = basm.HashTACStep(previous, a.BlockHash, a.BASMRoot)
		anchors[height] = a
		previous = a.TAC
	}
	fx.anchors = anchors
	fx.proofB = bumpBytes(2, [][]proofNode{{{offset: 0, hash: leaves[0]}, {offset: 1, hash: leaves[1], txid: true}}, {{offset: 1, hash: mustHash(t, "df2db416a05165238668a5d299b773f5f1bd00364b37c9d8b3d299367e4b3d64")}}})
	fx.proofC = bumpBytes(2, [][]proofNode{{{offset: 2, hash: leaves[2], txid: true}, {offset: 3, duplicate: true}}, {{offset: 0, hash: mustHash(t, "b4100303b9e99ada4b479b0bb93d9b549cb057a1c4be08896bc982debe20ce39")}}})
	proofA := bumpBytes(1, [][]proofNode{{{offset: 0, hash: leaves[0], txid: true}, {offset: 1, hash: leaves[1]}, {offset: 2, hash: leaves[2]}, {offset: 3, hash: leaves[3]}}})
	fx.view = &fakeBASMReadView{tip: anchorPtr(anchors[3]), anchors: anchors, admitted: map[uint32][]basm.AdmittedTxRef{0: {{TxID: genesisID, BlockIndex: 0}}, 1: {{TxID: leaves[0], BlockIndex: 0}}, 2: {{TxID: leaves[1], BlockIndex: 1}, {TxID: leaves[2], BlockIndex: 2}}, 3: {}}, proofs: map[basm.Hash][]byte{leaves[0]: proofA, leaves[1]: fx.proofB, leaves[2]: fx.proofC}, raw: map[basm.Hash][]byte{genesisID: raw}}
	return fx
}

func newReadService(t *testing.T, storage BASMReadOpener, headers BASMHeaderResolver, limits basm.ReadLimits) *BASMReadService {
	t.Helper()
	service, err := NewBASMReadService(storage, headers, limits)
	require.NoError(t, err)
	return service
}

func (f *readFixture) storage() BASMReadOpener {
	return &fakeBASMReadStorage{view: f.view, opens: &f.storageOpens}
}

type fakeBASMReadStorage struct {
	view  *fakeBASMReadView
	opens *int
	err   error
}

func (s *fakeBASMReadStorage) OpenBASMRead(_ context.Context, _ string, _ basm.ReadLimits) (BASMReadView, error) {
	*s.opens++
	if s.err != nil {
		return nil, s.err
	}
	return s.view, nil
}

type fakeBASMReadView struct {
	tip            *basm.Anchor
	anchors        map[uint32]basm.Anchor
	anchorOverride []basm.Anchor
	admitted       map[uint32][]basm.AdmittedTxRef
	proofs         map[basm.Hash][]byte
	raw            map[basm.Hash][]byte
	cancel         context.CancelFunc
	checkErr       error
	closeErr       error
	closed         int
}

func (v *fakeBASMReadView) Tip(_ context.Context) (*basm.Anchor, error) {
	if v.cancel != nil {
		v.cancel()
	}
	if v.tip == nil {
		return nil, nil //nolint:nilnil // this is the documented initialized empty-topic sentinel
	}
	result := *v.tip
	return &result, nil
}

func (v *fakeBASMReadView) Anchors(_ context.Context, from, to, _ uint32) ([]basm.Anchor, error) {
	if v.anchorOverride != nil {
		return append([]basm.Anchor(nil), v.anchorOverride...), nil
	}
	result := make([]basm.Anchor, 0, uint64(to)-uint64(from)+1)
	for height := from; ; height++ {
		anchor, exists := v.anchors[height]
		if !exists {
			return nil, ErrBASMNotReady
		}
		result = append(result, anchor)
		if height == to {
			break
		}
	}
	return result, nil
}

func (v *fakeBASMReadView) Admitted(_ context.Context, height, _ uint32) ([]basm.AdmittedTxRef, error) {
	return append([]basm.AdmittedTxRef(nil), v.admitted[height]...), nil
}

func (v *fakeBASMReadView) MerklePath(_ context.Context, txid basm.Hash, _ uint32) ([]byte, error) {
	proof, exists := v.proofs[txid]
	if !exists {
		return nil, ErrBASMNotFound
	}
	return append([]byte(nil), proof...), nil
}

func (v *fakeBASMReadView) RawTx(_ context.Context, txid basm.Hash, _ uint32) ([]byte, error) {
	raw, exists := v.raw[txid]
	if !exists {
		return nil, ErrBASMNotFound
	}
	return append([]byte(nil), raw...), nil
}

func (v *fakeBASMReadView) CheckCurrent(_ context.Context) error { return v.checkErr }
func (v *fakeBASMReadView) Close() error                         { v.closed++; return v.closeErr }

type fakeBASMHeaders struct {
	headers        map[uint32]BASMCanonicalHeader
	changed        map[uint32]BASMCanonicalHeader
	changeOnSecond map[uint32]bool
	calls          map[uint32]int
}

func (h *fakeBASMHeaders) CanonicalBASMHeader(_ context.Context, height uint32) (BASMCanonicalHeader, error) {
	h.calls[height]++
	if h.changeOnSecond[height] && h.calls[height] > 1 {
		return h.changed[height], nil
	}
	header, exists := h.headers[height]
	if !exists {
		return BASMCanonicalHeader{}, ErrBASMNotReady
	}
	return header, nil
}

type proofNode struct {
	offset    uint64
	hash      basm.Hash
	txid      bool
	duplicate bool
}

func bumpBytes(height uint32, levels [][]proofNode) []byte {
	path := make([][]*transaction.PathElement, len(levels))
	for level, nodes := range levels {
		path[level] = make([]*transaction.PathElement, 0, len(nodes))
		for _, node := range nodes {
			element := &transaction.PathElement{Offset: node.offset}
			if node.duplicate {
				value := true
				element.Duplicate = &value
			} else {
				hash := chainhash.Hash(node.hash)
				element.Hash = &hash
			}
			if node.txid {
				value := true
				element.Txid = &value
			}
			path[level] = append(path[level], element)
		}
	}
	return transaction.NewMerklePath(height, path).Bytes()
}

func repeatHash(value byte) basm.Hash {
	var hash basm.Hash
	for i := range hash {
		hash[i] = value
	}
	return hash
}

func mustHash(t *testing.T, display string) basm.Hash {
	t.Helper()
	hash, err := basm.ParseHash(display)
	require.NoError(t, err)
	return hash
}

func anchorPtr(anchor basm.Anchor) *basm.Anchor { return &anchor }

func proofCap(first, second []byte) uint32 {
	return uint32(len(first) + len(second) - 1) //nolint:gosec // inline test BUMPs are far below uint32
}
