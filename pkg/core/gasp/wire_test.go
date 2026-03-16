package gasp

import (
	"crypto/rand"
	"testing"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

func randomHash(t *testing.T) chainhash.Hash {
	t.Helper()
	var h chainhash.Hash
	if _, err := rand.Read(h[:]); err != nil {
		t.Fatal(err)
	}
	return h
}

func randomHashPtr(t *testing.T) *chainhash.Hash {
	t.Helper()
	h := randomHash(t)
	return &h
}

func TestInitialRequestWireRoundTrip(t *testing.T) {
	req := &InitialRequest{Version: 1, Since: 1710460800.0, Limit: 100}
	data := req.Serialize()
	got, err := DeserializeInitialRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != req.Version {
		t.Errorf("version: want %d, got %d", req.Version, got.Version)
	}
	if got.Since != req.Since {
		t.Errorf("since: want %f, got %f", req.Since, got.Since)
	}
	if got.Limit != req.Limit {
		t.Errorf("limit: want %d, got %d", req.Limit, got.Limit)
	}
}

func TestInitialResponseWireRoundTrip(t *testing.T) {
	resp := &InitialResponse{
		Since: 1710460800.0,
		UTXOList: []*Output{
			{Txid: randomHash(t), OutputIndex: 0, Score: 100.0},
			{Txid: randomHash(t), OutputIndex: 1, Score: 200.5},
			{Txid: randomHash(t), OutputIndex: 3, Score: 300.0},
		},
	}
	data := resp.Serialize()
	got, err := DeserializeInitialResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Since != resp.Since {
		t.Errorf("since: want %f, got %f", resp.Since, got.Since)
	}
	if len(got.UTXOList) != 3 {
		t.Fatalf("utxo count: want 3, got %d", len(got.UTXOList))
	}
	for i, u := range got.UTXOList {
		if u.Txid != resp.UTXOList[i].Txid {
			t.Errorf("utxo[%d] txid mismatch", i)
		}
		if u.OutputIndex != resp.UTXOList[i].OutputIndex {
			t.Errorf("utxo[%d] index: want %d, got %d", i, resp.UTXOList[i].OutputIndex, u.OutputIndex)
		}
		if u.Score != resp.UTXOList[i].Score {
			t.Errorf("utxo[%d] score: want %f, got %f", i, resp.UTXOList[i].Score, u.Score)
		}
	}
}

func TestInitialResponseWireEmpty(t *testing.T) {
	resp := &InitialResponse{Since: 0}
	data := resp.Serialize()
	got, err := DeserializeInitialResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.UTXOList) != 0 {
		t.Errorf("expected empty UTXO list")
	}
}

func TestInitialReplyWireRoundTrip(t *testing.T) {
	reply := &InitialReply{
		UTXOList: []*Output{
			{Txid: randomHash(t), OutputIndex: 0, Score: 50.0},
		},
	}
	data := reply.Serialize()
	got, err := DeserializeInitialReply(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.UTXOList) != 1 {
		t.Fatalf("utxo count: want 1, got %d", len(got.UTXOList))
	}
	if got.UTXOList[0].Score != 50.0 {
		t.Errorf("score: want 50.0, got %f", got.UTXOList[0].Score)
	}
}

func TestNodeRequestWireRoundTrip(t *testing.T) {
	graphID := &transaction.Outpoint{Txid: randomHash(t), Index: 0}
	txid := randomHashPtr(t)
	req := &NodeRequest{
		GraphID:     graphID,
		Txid:        txid,
		OutputIndex: 2,
		Metadata:    true,
	}
	data := req.Serialize()
	if len(data) != 74 {
		t.Fatalf("expected 74 bytes, got %d", len(data))
	}
	got, err := DeserializeNodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.GraphID.Txid != graphID.Txid || got.GraphID.Index != graphID.Index {
		t.Errorf("graphID mismatch")
	}
	if *got.Txid != *txid {
		t.Errorf("txid mismatch")
	}
	if got.OutputIndex != 2 {
		t.Errorf("outputIndex: want 2, got %d", got.OutputIndex)
	}
	if !got.Metadata {
		t.Errorf("metadata: want true, got false")
	}
}

func TestNodeWireRoundTrip(t *testing.T) {
	proof := "aabbccdd"
	node := &Node{
		GraphID:        &transaction.Outpoint{Txid: randomHash(t), Index: 0},
		RawTx:          "0100000001abcdef",
		OutputIndex:    1,
		Proof:          &proof,
		TxMetadata:     "tx-meta",
		OutputMetadata: "out-meta",
		Inputs: map[string]*Input{
			"input-hash-1": {Hash: "input-hash-1"},
			"input-hash-2": {Hash: "input-hash-2"},
		},
	}

	data, err := node.Serialize()
	if err != nil {
		t.Fatal(err)
	}

	got, err := DeserializeNode(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.GraphID.Txid != node.GraphID.Txid {
		t.Errorf("graphID txid mismatch")
	}
	if got.OutputIndex != node.OutputIndex {
		t.Errorf("outputIndex: want %d, got %d", node.OutputIndex, got.OutputIndex)
	}
	if got.RawTx != node.RawTx {
		t.Errorf("rawTx: want %q, got %q", node.RawTx, got.RawTx)
	}
	if got.Proof == nil || *got.Proof != proof {
		t.Errorf("proof mismatch")
	}
	if got.TxMetadata != node.TxMetadata {
		t.Errorf("txMetadata: want %q, got %q", node.TxMetadata, got.TxMetadata)
	}
	if got.OutputMetadata != node.OutputMetadata {
		t.Errorf("outputMetadata: want %q, got %q", node.OutputMetadata, got.OutputMetadata)
	}
	if len(got.Inputs) != 2 {
		t.Fatalf("inputs: want 2, got %d", len(got.Inputs))
	}
	for hash := range node.Inputs {
		if _, ok := got.Inputs[hash]; !ok {
			t.Errorf("missing input %q", hash)
		}
	}
}

func TestNodeWireEmpty(t *testing.T) {
	node := &Node{
		GraphID:     &transaction.Outpoint{Txid: randomHash(t), Index: 0},
		OutputIndex: 0,
	}

	data, err := node.Serialize()
	if err != nil {
		t.Fatal(err)
	}

	got, err := DeserializeNode(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.RawTx != "" {
		t.Errorf("expected empty rawTx")
	}
	if got.Proof != nil {
		t.Errorf("expected nil proof")
	}
	if len(got.Inputs) != 0 {
		t.Errorf("expected empty inputs")
	}
}

func TestNodeResponseWireRoundTrip(t *testing.T) {
	resp := &NodeResponse{
		RequestedInputs: map[transaction.Outpoint]*NodeResponseData{
			{Txid: randomHash(t), Index: 0}: {Metadata: true},
			{Txid: randomHash(t), Index: 1}: {Metadata: false},
		},
	}

	data := resp.Serialize()
	got, err := DeserializeNodeResponse(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.RequestedInputs) != 2 {
		t.Fatalf("count: want 2, got %d", len(got.RequestedInputs))
	}

	for outpoint, wantData := range resp.RequestedInputs {
		gotData, ok := got.RequestedInputs[outpoint]
		if !ok {
			t.Errorf("missing outpoint %s", outpoint.String())
			continue
		}
		if gotData.Metadata != wantData.Metadata {
			t.Errorf("outpoint %s metadata: want %v, got %v", outpoint.String(), wantData.Metadata, gotData.Metadata)
		}
	}
}

func TestNodeResponseWireEmpty(t *testing.T) {
	resp := &NodeResponse{
		RequestedInputs: map[transaction.Outpoint]*NodeResponseData{},
	}

	data := resp.Serialize()
	got, err := DeserializeNodeResponse(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.RequestedInputs) != 0 {
		t.Errorf("expected empty inputs")
	}
}
