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
	tsRoot := os.Getenv("BASM_TS_STACK")
	if tsRoot == "" {
		t.Skip("set BASM_TS_STACK to an absolute TypeScript stack checkout to run BASMRemote interop")
	}
	require.True(t, filepath.IsAbs(tsRoot), "BASM_TS_STACK must be absolute")
	tsxPath := filepath.Join(tsRoot, "node_modules", ".bin", "tsx")
	remotePath := filepath.Join(tsRoot, "packages", "overlays", "overlay", "src", "BASMRemote.ts")
	overlayPackage := filepath.Dir(filepath.Dir(remotePath))
	for _, path := range []string{tsxPath, remotePath} {
		info, err := os.Stat(path) //nolint:gosec // test reads only the user-selected external checkout.
		require.NoError(t, err, "BASM_TS_STACK is missing %s", path)
		require.False(t, info.IsDir(), "expected file at %s", path)
	}

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
