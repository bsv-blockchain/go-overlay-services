package server

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/adapters"
)

const (
	interopTopic        = "tm_interop"
	interopGenesisID    = "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"
	interopGenesisHex   = "01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff4d04ffff001d0104455468652054696d65732030332f4a616e2f32303039204368616e63656c6c6f72206f6e206272696e6b206f66207365636f6e64206261696c6f757420666f722062616e6b73ffffffff0100f2052a01000000434104678afdb0fe5548271967f1a67130b7105cd6a828e03909a67962e0ea1f61deb649f6bc3f4cef38c4f35504e51ec112de5c384df7ba0b8d578a4c702b6bf11d5fac00000000"
	interopGenesisBlock = "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f"
)

// TestBASMRemoteInterop runs the checked-out TypeScript BASMRemote client against
// real Fiber HTTP listeners and a real engine.BASMReadService. It is opt-in because
// this repository does not own the TypeScript checkout or its installed runtime.
func TestBASMRemoteInterop(t *testing.T) {
	tsxPath, remotePath, overlayPackage := basmRemoteInteropRuntime(t)

	service, txid := newInteropBASMService(t, true)
	readyURL := startInteropServer(t, service)
	unsupportedURL := startInteropServer(t, nil)
	notReadyService, _ := newInteropBASMService(t, false)
	notReadyURL := startInteropServer(t, notReadyService)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, tsxPath, "-e", basmRemoteInteropScript(remotePath, readyURL, unsupportedURL, notReadyURL, txid.String())) //nolint:gosec // opt-in test executes the user-selected tsx binary only.
	command.Dir = overlayPackage
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "TS BASMRemote interop failed: %s", output)
	if err = ctx.Err(); err != nil {
		t.Fatalf("TS BASMRemote interop timed out: %v", err)
	}
}

// TestBASMRemoteProofInterop checks BRC-74 proof shapes through the real TS
// client and SDK. The hashes and headers below are synthetic canonical test
// fixtures, not claims about a live Bitcoin block.
func TestBASMRemoteProofInterop(t *testing.T) {
	tsxPath, remotePath, overlayPackage := basmRemoteInteropRuntime(t)
	service, fixture := newInteropProofBASMService(t, false)
	readyURL := startInteropServer(t, service)
	invalidService, _ := newInteropProofBASMService(t, true)
	invalidURL := startInteropServer(t, invalidService)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	//nolint:gosec // opt-in test executes the user-selected tsx binary only.
	command := exec.CommandContext(ctx, tsxPath, "-e", basmRemoteProofInteropScript(
		remotePath,
		readyURL,
		invalidURL,
		fixture.fourLeaves[0].String(),
		fixture.fourLeaves[1].String(),
		fixture.fourLeaves[2].String(),
		fixture.fourLeaves[3].String(),
		fixture.fourRoot.String(),
		fixture.oddLeaves[2].String(),
		fixture.oddRoot.String(),
	))
	command.Dir = overlayPackage
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "TS BASMRemote proof interop failed: %s", output)
	if err = ctx.Err(); err != nil {
		t.Fatalf("TS BASMRemote proof interop timed out: %v", err)
	}
}

func basmRemoteInteropRuntime(t *testing.T) (tsxPath, remotePath, overlayPackage string) {
	t.Helper()
	tsRoot := os.Getenv("BASM_TS_STACK")
	if tsRoot == "" {
		t.Skip("set BASM_TS_STACK to an absolute TypeScript stack checkout to run BASMRemote interop")
	}
	require.True(t, filepath.IsAbs(tsRoot), "BASM_TS_STACK must be absolute")
	tsxPath = filepath.Join(tsRoot, "node_modules", ".bin", "tsx")
	remotePath = filepath.Join(tsRoot, "packages", "overlays", "overlay", "src", "BASMRemote.ts")
	overlayPackage = filepath.Dir(filepath.Dir(remotePath))
	for _, path := range []string{tsxPath, remotePath} {
		info, err := os.Stat(path) //nolint:gosec // test reads only the user-selected external checkout.
		require.NoError(t, err, "BASM_TS_STACK is missing %s", path)
		require.False(t, info.IsDir(), "expected file at %s", path)
	}
	return tsxPath, remotePath, overlayPackage
}

func basmRemoteInteropScript(remotePath, readyURL, unsupportedURL, notReadyURL, txid string) string {
	return fmt.Sprintf(`
import { BASMRemote } from %s
import { MerklePath, Transaction } from '@bsv/sdk'

const run = async (): Promise<void> => {
  const topic = %s
  const txid = %s
  const missing = 'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff'
  const rawHex = %s
  const ready = new BASMRemote(%s, topic)
  const tip = await ready.requestTopicAnchorTip()
  const range = await ready.requestTopicAnchorRange(0, 0)
  const admitted = await ready.requestAdmittedList(0)
  const proof = await ready.requestCompoundMerklePath(0, [txid])
  const raw = await ready.requestRawTransactions([txid, missing])

  if (tip.topic !== topic || tip.blockHeight !== 0) throw new Error('tip mismatch')
  if (range.topic !== topic || range.anchors.length !== 1 || range.anchors[0].blockHeight !== 0) throw new Error('range mismatch')
  if (admitted.topic !== topic || admitted.admitted.length !== 1 || admitted.admitted[0].txid !== txid) throw new Error('admitted list mismatch')
  const parsedBump = MerklePath.fromHex(proof.merklePath)
  if (proof.topic !== topic || proof.blockHeight !== 0 || proof.txids[0] !== txid || parsedBump.blockHeight !== 0 || parsedBump.path[0][0].hash !== txid || parsedBump.computeRoot(txid) !== txid) throw new Error('BUMP parse mismatch')
  const parsedRaw = Transaction.fromHex(raw.transactions[0].rawTx)
  if (raw.transactions.length !== 1 || raw.transactions[0].txid !== txid || raw.transactions[0].rawTx !== rawHex || parsedRaw.id('hex') !== txid || raw.missing.length !== 1 || raw.missing[0] !== missing) throw new Error('raw transaction parse mismatch')

  for (const [endpoint, expectedStatus] of [[%s, 501], [%s, 503]] as const) {
    try {
      await new BASMRemote(endpoint, topic).requestTopicAnchorTip()
      throw new Error('expected BASMRemote request to fail')
    } catch (error) {
      if (!String(error).includes(String(expectedStatus))) throw error
    }
  }
}
run().then(() => console.log('BASMRemote interop passed')).catch(error => { console.error(error); process.exitCode = 1 })
`, strconv.Quote(remotePath), strconv.Quote(interopTopic), strconv.Quote(txid), strconv.Quote(interopGenesisHex), strconv.Quote(readyURL), strconv.Quote(unsupportedURL), strconv.Quote(notReadyURL))
}

func basmRemoteProofInteropScript(remotePath, readyURL, invalidURL, tx0, tx1, tx2, tx3, root4, odd2, root3 string) string {
	return fmt.Sprintf(`
import { BASMRemote } from %s
import { MerklePath } from '@bsv/sdk'

const run = async (): Promise<void> => {
  const topic = %s
  const four = [%s, %s, %s, %s]
  const root4 = %s
  const odd2 = %s
  const root3 = %s
  const ready = new BASMRemote(%s, topic)
  const assertProof = (proof: { txids: string[], merklePath: string }, txids: string[], root: string, label: string): MerklePath => {
    if (proof.txids.length !== txids.length || proof.txids.some((txid, index) => txid !== txids[index])) throw new Error(label + ' response IDs mismatch')
    const parsed = MerklePath.fromHex(proof.merklePath)
    for (const txid of txids) {
      if (parsed.computeRoot(txid) !== root) throw new Error(label + ' root mismatch for ' + txid)
    }
    return parsed
  }

  // Four leaves exercise the valid two-level shape: level 0 {0,1}, level 1 {1:H(2,3)}.
  const singleton = await ready.requestCompoundMerklePath(41, [four[0]])
  const singletonPath = assertProof(singleton, [four[0]], root4, 'four-leaf singleton')
  const partialOther = await ready.requestCompoundMerklePath(41, [four[2]])
  const partialOtherPath = assertProof(partialOther, [four[2]], root4, 'four-leaf second partial')
  singletonPath.combine(partialOtherPath)
  if (singletonPath.computeRoot(four[0]) !== root4 || singletonPath.computeRoot(four[2]) !== root4) throw new Error('partial proof union root mismatch')

  const subset = await ready.requestCompoundMerklePath(41, [four[0], four[2]])
  assertProof(subset, [four[0], four[2]], root4, 'four-leaf subset')
  const all = await ready.requestCompoundMerklePath(41, four)
  assertProof(all, four, root4, 'four-leaf multiple')

  const odd = await ready.requestCompoundMerklePath(42, [odd2])
  const oddPath = assertProof(odd, [odd2], root3, 'odd-width duplicate')
  if (!oddPath.path[0].some(node => node.offset === 3 && node.duplicate === true)) throw new Error('odd-width duplicate marker missing')

  try {
    await new BASMRemote(%s, topic).requestCompoundMerklePath(41, [four[0]])
    throw new Error('expected overcomplete proof provider to fail')
  } catch (error) {
    if (!String(error).includes('500')) throw error
  }
}
run().then(() => console.log('BASMRemote proof interop passed')).catch(error => { console.error(error); process.exitCode = 1 })
`, strconv.Quote(remotePath), strconv.Quote(interopTopic), strconv.Quote(tx0), strconv.Quote(tx1), strconv.Quote(tx2), strconv.Quote(tx3), strconv.Quote(root4), strconv.Quote(odd2), strconv.Quote(root3), strconv.Quote(readyURL), strconv.Quote(invalidURL))
}

func startInteropServer(t *testing.T, provider engine.BASMProvider) string {
	t.Helper()
	app := RegisterRoutesWithErrorHandler(fiber.New(), &RegisterRoutesConfig{
		AdminBearerToken: "interop-admin-token",
		Engine:           adapters.NewNoopEngineProvider(),
		BASMProvider:     provider,
		BASMLimits:       basm.DefaultReadLimits(),
	})
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveDone := make(chan error, 1)
	go func() { serveDone <- app.Listener(listener) }()
	t.Cleanup(func() {
		if shutdownErr := app.Shutdown(); shutdownErr != nil {
			t.Errorf("shutdown interop Fiber server: %v", shutdownErr)
		}
		if serveErr := <-serveDone; serveErr != nil && !errors.Is(serveErr, net.ErrClosed) {
			t.Errorf("serve interop Fiber server: %v", serveErr)
		}
	})
	return "http://" + listener.Addr().String()
}

func newInteropBASMService(t *testing.T, withHeaders bool) (*engine.BASMReadService, basm.Hash) {
	t.Helper()
	txid := interopHash(t, interopGenesisID)
	blockHash := interopHash(t, interopGenesisBlock)
	txidFlag := true
	transactionHash := chainhash.Hash(txid)
	proof := transaction.NewMerklePath(0, [][]*transaction.PathElement{{{
		Offset: 0,
		Hash:   &transactionHash,
		Txid:   &txidFlag,
	}}}).Bytes()
	anchor := basm.Anchor{TopicBlockAnchor: basm.TopicBlockAnchor{
		Topic:         interopTopic,
		BlockHeight:   0,
		BlockHash:     blockHash,
		BASMRoot:      txid,
		AdmittedCount: 1,
	}}
	anchor.TAC = basm.HashTACStep(basm.Hash{}, anchor.BlockHash, anchor.BASMRoot)
	storage := interopBASMStorage{view: interopBASMView{anchor: anchor, txid: txid, proof: proof}}
	var headers engine.BASMHeaderResolver
	if withHeaders {
		headers = interopBASMHeaders{header: engine.BASMCanonicalHeader{Height: 0, BlockHash: blockHash, MerkleRoot: txid, TransactionCount: 1}}
	}
	service, err := engine.NewBASMReadService(storage, headers, basm.DefaultReadLimits())
	require.NoError(t, err)
	return service, txid
}

func interopHash(t *testing.T, display string) basm.Hash {
	t.Helper()
	hash, err := basm.ParseHash(display)
	require.NoError(t, err)
	return hash
}

type interopBASMStorage struct{ view interopBASMView }

func (s interopBASMStorage) OpenBASMRead(_ context.Context, topic string, _ basm.ReadLimits) (engine.BASMReadView, error) {
	if topic != "" && topic != interopTopic {
		return nil, engine.ErrBASMNotFound
	}
	return s.view, nil
}

type interopBASMView struct {
	anchor basm.Anchor
	txid   basm.Hash
	proof  []byte
}

func (v interopBASMView) Tip(context.Context) (*basm.Anchor, error) {
	anchor := v.anchor
	return &anchor, nil
}

func (v interopBASMView) Anchors(_ context.Context, from, to, _ uint32) ([]basm.Anchor, error) {
	if from != 0 || to != 0 {
		return nil, engine.ErrBASMNotReady
	}
	return []basm.Anchor{v.anchor}, nil
}

func (v interopBASMView) Admitted(_ context.Context, height, _ uint32) ([]basm.AdmittedTxRef, error) {
	if height != 0 {
		return nil, engine.ErrBASMNotReady
	}
	return []basm.AdmittedTxRef{{TxID: v.txid, BlockIndex: 0}}, nil
}

func (v interopBASMView) MerklePath(_ context.Context, txid basm.Hash, _ uint32) ([]byte, error) {
	if txid != v.txid {
		return nil, engine.ErrBASMNotFound
	}
	return append([]byte(nil), v.proof...), nil
}

func (v interopBASMView) RawTx(_ context.Context, txid basm.Hash, _ uint32) ([]byte, error) {
	if txid != v.txid {
		return nil, engine.ErrBASMNotFound
	}
	raw, err := hexDecodeInteropGenesis()
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (interopBASMView) CheckCurrent(context.Context) error { return nil }
func (interopBASMView) Close() error                       { return nil }

type interopBASMHeaders struct{ header engine.BASMCanonicalHeader }

func (h interopBASMHeaders) CanonicalBASMHeader(_ context.Context, height uint32) (engine.BASMCanonicalHeader, error) {
	if height != h.header.Height {
		return engine.BASMCanonicalHeader{}, engine.ErrBASMNotReady
	}
	return h.header, nil
}

func hexDecodeInteropGenesis() ([]byte, error) {
	return hex.DecodeString(interopGenesisHex)
}

type interopProofFixture struct {
	fourLeaves []basm.Hash
	fourRoot   basm.Hash
	oddLeaves  []basm.Hash
	oddRoot    basm.Hash
}

func newInteropProofBASMService(t *testing.T, overcomplete bool) (*engine.BASMReadService, interopProofFixture) {
	t.Helper()
	fixture := interopProofFixture{
		fourLeaves: []basm.Hash{interopSyntheticHash(0x11), interopSyntheticHash(0x12), interopSyntheticHash(0x13), interopSyntheticHash(0x14)},
		oddLeaves:  []basm.Hash{interopSyntheticHash(0x21), interopSyntheticHash(0x22), interopSyntheticHash(0x23)},
	}
	fixture.fourRoot = interopMerkleRoot(t, fixture.fourLeaves)
	fixture.oddRoot = interopMerkleRoot(t, fixture.oddLeaves)
	fourLeft := interopMerkleRoot(t, fixture.fourLeaves[:2])
	fourRight := interopMerkleRoot(t, fixture.fourLeaves[2:])
	oddLeft := interopMerkleRoot(t, fixture.oddLeaves[:2])

	fourAnchor := interopProofAnchor(41, interopSyntheticHash(0x81), fixture.fourRoot, uint64(len(fixture.fourLeaves)))
	oddAnchor := interopProofAnchor(42, interopSyntheticHash(0x82), fixture.oddRoot, uint64(len(fixture.oddLeaves)))
	proofs := map[basm.Hash][]byte{
		fixture.fourLeaves[0]: interopBUMPBytes(41, [][]interopProofNode{
			{{offset: 0, hash: fixture.fourLeaves[0], txid: true}, {offset: 1, hash: fixture.fourLeaves[1]}},
			{{offset: 1, hash: fourRight}},
		}),
		fixture.fourLeaves[1]: interopBUMPBytes(41, [][]interopProofNode{
			{{offset: 0, hash: fixture.fourLeaves[0]}, {offset: 1, hash: fixture.fourLeaves[1], txid: true}},
			{{offset: 1, hash: fourRight}},
		}),
		fixture.fourLeaves[2]: interopBUMPBytes(41, [][]interopProofNode{
			{{offset: 2, hash: fixture.fourLeaves[2], txid: true}, {offset: 3, hash: fixture.fourLeaves[3]}},
			{{offset: 0, hash: fourLeft}},
		}),
		fixture.fourLeaves[3]: interopBUMPBytes(41, [][]interopProofNode{
			{{offset: 2, hash: fixture.fourLeaves[2]}, {offset: 3, hash: fixture.fourLeaves[3], txid: true}},
			{{offset: 0, hash: fourLeft}},
		}),
		fixture.oddLeaves[2]: interopBUMPBytes(42, [][]interopProofNode{
			{{offset: 2, hash: fixture.oddLeaves[2], txid: true}, {offset: 3, duplicate: true}},
			{{offset: 0, hash: oddLeft}},
		}),
	}
	if overcomplete {
		// This repeats a parent already derivable from level 0 {0,1}. It is
		// syntactically parseable but not a valid BRC-74 partial proof shape.
		proofs[fixture.fourLeaves[0]] = interopBUMPBytes(41, [][]interopProofNode{
			{{offset: 0, hash: fixture.fourLeaves[0], txid: true}, {offset: 1, hash: fixture.fourLeaves[1]}},
			{{offset: 0, hash: fourLeft}, {offset: 1, hash: fourRight}},
		})
	}

	view := interopProofView{
		anchors: map[uint32]basm.Anchor{41: fourAnchor, 42: oddAnchor},
		admitted: map[uint32][]basm.AdmittedTxRef{
			41: interopAdmitted(fixture.fourLeaves),
			42: interopAdmitted(fixture.oddLeaves),
		},
		proofs: proofs,
	}
	storage := interopProofStorage{view: view}
	headers := interopProofHeaders{headers: map[uint32]engine.BASMCanonicalHeader{
		41: {Height: 41, BlockHash: fourAnchor.BlockHash, MerkleRoot: fixture.fourRoot, TransactionCount: uint64(len(fixture.fourLeaves))},
		42: {Height: 42, BlockHash: oddAnchor.BlockHash, MerkleRoot: fixture.oddRoot, TransactionCount: uint64(len(fixture.oddLeaves))},
	}}
	service, err := engine.NewBASMReadService(storage, headers, basm.DefaultReadLimits())
	require.NoError(t, err)
	return service, fixture
}

func interopSyntheticHash(seed byte) basm.Hash {
	var hash basm.Hash
	for index := range hash {
		hash[index] = seed + byte(index)
	}
	return hash
}

func interopMerkleRoot(t *testing.T, leaves []basm.Hash) basm.Hash {
	t.Helper()
	root, err := basm.Root(context.Background(), leaves, uint32(len(leaves))) //nolint:gosec // fixed small test fixtures.
	require.NoError(t, err)
	return root
}

func interopProofAnchor(height uint32, blockHash, root basm.Hash, count uint64) basm.Anchor {
	anchor := basm.Anchor{TopicBlockAnchor: basm.TopicBlockAnchor{
		Topic:         interopTopic,
		BlockHeight:   height,
		BlockHash:     blockHash,
		BASMRoot:      root,
		AdmittedCount: count,
	}}
	anchor.TAC = basm.HashTACStep(basm.Hash{}, anchor.BlockHash, anchor.BASMRoot)
	return anchor
}

func interopAdmitted(leaves []basm.Hash) []basm.AdmittedTxRef {
	admitted := make([]basm.AdmittedTxRef, len(leaves))
	for index, txid := range leaves {
		admitted[index] = basm.AdmittedTxRef{TxID: txid, BlockIndex: uint64(index)}
	}
	return admitted
}

type interopProofNode struct {
	offset    uint64
	hash      basm.Hash
	txid      bool
	duplicate bool
}

func interopBUMPBytes(height uint32, levels [][]interopProofNode) []byte {
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

type interopProofStorage struct{ view interopProofView }

func (s interopProofStorage) OpenBASMRead(_ context.Context, topic string, _ basm.ReadLimits) (engine.BASMReadView, error) {
	if topic != "" && topic != interopTopic {
		return nil, engine.ErrBASMNotFound
	}
	return s.view, nil
}

type interopProofView struct {
	anchors  map[uint32]basm.Anchor
	admitted map[uint32][]basm.AdmittedTxRef
	proofs   map[basm.Hash][]byte
}

func (v interopProofView) Tip(context.Context) (*basm.Anchor, error) {
	anchor := v.anchors[42]
	return &anchor, nil
}

func (v interopProofView) Anchors(_ context.Context, from, to, _ uint32) ([]basm.Anchor, error) {
	if from != to {
		return nil, engine.ErrBASMNotReady
	}
	anchor, ok := v.anchors[from]
	if !ok {
		return nil, engine.ErrBASMNotReady
	}
	return []basm.Anchor{anchor}, nil
}

func (v interopProofView) Admitted(_ context.Context, height, _ uint32) ([]basm.AdmittedTxRef, error) {
	admitted, ok := v.admitted[height]
	if !ok {
		return nil, engine.ErrBASMNotReady
	}
	return append([]basm.AdmittedTxRef(nil), admitted...), nil
}

func (v interopProofView) MerklePath(_ context.Context, txid basm.Hash, _ uint32) ([]byte, error) {
	proof, ok := v.proofs[txid]
	if !ok {
		return nil, engine.ErrBASMNotFound
	}
	return append([]byte(nil), proof...), nil
}

func (interopProofView) RawTx(context.Context, basm.Hash, uint32) ([]byte, error) {
	return nil, engine.ErrBASMNotFound
}

func (interopProofView) CheckCurrent(context.Context) error { return nil }
func (interopProofView) Close() error                       { return nil }

type interopProofHeaders struct {
	headers map[uint32]engine.BASMCanonicalHeader
}

func (h interopProofHeaders) CanonicalBASMHeader(_ context.Context, height uint32) (engine.BASMCanonicalHeader, error) {
	header, ok := h.headers[height]
	if !ok {
		return engine.BASMCanonicalHeader{}, engine.ErrBASMNotReady
	}
	return header, nil
}
