package basm_test

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type conformanceVectors struct {
	SpecRevision struct {
		BRC136   string `json:"brc136"`
		RepoHead string `json:"repoHead"`
	} `json:"specRevision"`
	ByteOrder []struct {
		Name     string `json:"name"`
		Display  string `json:"display"`
		Internal string `json:"internal"`
	} `json:"byteOrder"`
	Merkle []struct {
		Name               string   `json:"name"`
		TxIDs              []string `json:"txids"`
		Root               string   `json:"root"`
		AdmissionListValid bool     `json:"admissionListValid"`
	} `json:"merkle"`
	TAC []struct {
		Name          string `json:"name"`
		GenesisHeight uint32 `json:"genesisHeight"`
		Anchors       []struct {
			BlockHeight   uint32 `json:"blockHeight"`
			BlockHash     string `json:"blockHash"`
			BASMRoot      string `json:"basmRoot"`
			AdmittedCount uint64 `json:"admittedCount"`
			ExpectedTAC   string `json:"expectedTac"`
		} `json:"anchors"`
	} `json:"tac"`
}

func frozenVectors(t *testing.T) conformanceVectors {
	t.Helper()
	data, err := os.ReadFile("testdata/vectors.json")
	require.NoError(t, err)
	var vectors conformanceVectors
	require.NoError(t, json.Unmarshal(data, &vectors))
	require.Equal(t, "2733cd2950a739b3c977b95d652ff63e3773c40b", vectors.SpecRevision.BRC136)
	require.NotEmpty(t, vectors.Merkle)
	require.NotEmpty(t, vectors.TAC)
	return vectors
}

func mustHash(t *testing.T, display string) basm.Hash {
	t.Helper()
	hash, err := basm.ParseHash(display)
	require.NoError(t, err)
	return hash
}

func TestIndependentBRC136MerkleVectors(t *testing.T) {
	vectors := frozenVectors(t)
	for _, vector := range vectors.ByteOrder {
		t.Run(vector.Name, func(t *testing.T) {
			hash := mustHash(t, vector.Display)
			assert.Equal(t, vector.Internal, hex.EncodeToString(hash[:]))
		})
	}
	for _, vector := range vectors.Merkle {
		t.Run(vector.Name, func(t *testing.T) {
			leaves := make([]basm.Hash, len(vector.TxIDs))
			refs := make([]basm.AdmittedTxRef, len(vector.TxIDs))
			for i, txid := range vector.TxIDs {
				leaves[i] = mustHash(t, txid)
				// Synthetic block positions intentionally contain gaps; they
				// are not BASM positions and must not create null leaves.
				refs[i] = basm.AdmittedTxRef{TxID: leaves[i], BlockIndex: uint64(i) * 7}
			}
			before := slices.Clone(leaves)
			root, err := basm.Root(context.Background(), leaves, basm.DefaultLimits().MaxAdmitted)
			require.NoError(t, err)
			assert.Equal(t, vector.Root, root.String())
			assert.True(t, slices.Equal(before, leaves), "hashing mutated input")
			listRoot, err := basm.ValidateAdmittedList(context.Background(), refs, 100, basm.DefaultLimits())
			if !vector.AdmissionListValid {
				require.Error(t, err, "duplicate primitive vector accepted as an admission list")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, vector.Root, listRoot.String())
			if len(leaves) > 1 && vector.AdmissionListValid {
				slices.Reverse(leaves)
				reversed, err := basm.Root(context.Background(), leaves, basm.DefaultLimits().MaxAdmitted)
				require.NoError(t, err)
				assert.NotEqual(t, root, reversed, "order must affect this asymmetric vector")
			}
		})
	}
}

func TestIndependentBRC136TACVectors(t *testing.T) {
	for _, vector := range frozenVectors(t).TAC {
		t.Run(vector.Name, func(t *testing.T) {
			chain, err := basm.NewChain("tm_fixture", vector.GenesisHeight, basm.DefaultLimits())
			require.NoError(t, err)
			var previous basm.Hash
			for _, row := range vector.Anchors {
				anchor := basm.TopicBlockAnchor{
					Topic: "tm_fixture", BlockHeight: row.BlockHeight,
					BlockHash: mustHash(t, row.BlockHash), BASMRoot: mustHash(t, row.BASMRoot),
					AdmittedCount: row.AdmittedCount,
				}
				step := basm.HashTACStep(previous, anchor.BlockHash, anchor.BASMRoot)
				assert.Equal(t, row.ExpectedTAC, step.String())
				chain, err = chain.Append(anchor)
				require.NoError(t, err)
				height, tac, ok := chain.Tip()
				assert.True(t, ok)
				assert.Equal(t, row.BlockHeight, height)
				assert.Equal(t, row.ExpectedTAC, tac.String())
				previous = step
			}
		})
	}
}

func TestProposedAdmissionLimitRepresentativePayload(t *testing.T) {
	limits := basm.DefaultLimits()
	refs := representativeList(limits.MaxAdmitted)
	data, err := json.Marshal(refs)
	require.NoError(t, err)
	require.LessOrEqual(t, uint64(len(data)), uint64(limits.MaxJSONBytes))
	t.Logf("%d admissions use %d JSON bytes (limit %d)", len(refs), len(data), limits.MaxJSONBytes)
	decoded, err := basm.DecodeAdmittedListJSON(context.Background(), data, uint64(limits.MaxAdmitted)*2, limits)
	require.NoError(t, err)
	assert.Equal(t, refs, decoded)
	limits.MaxAdmitted--
	_, err = basm.DecodeAdmittedListJSON(context.Background(), data, uint64(len(refs))*2, limits)
	require.Error(t, err)
}

func representativeList(count uint32) []basm.AdmittedTxRef {
	refs := make([]basm.AdmittedTxRef, count)
	for i := range refs {
		binary.LittleEndian.PutUint64(refs[i].TxID[:8], uint64(i)+1)
		refs[i].BlockIndex = uint64(i) * 2
	}
	return refs
}

func BenchmarkValidateAdmittedListProposedLimit(b *testing.B) {
	limits := basm.DefaultLimits()
	refs := representativeList(limits.MaxAdmitted)
	b.ReportAllocs()
	for b.Loop() {
		_, err := basm.ValidateAdmittedList(context.Background(), refs, uint64(limits.MaxAdmitted)*2, limits)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzBoundedJSONDecoders(f *testing.F) {
	f.Add([]byte(`{"fromHeight":0,"toHeight":0}`))
	f.Add([]byte(`[{"txid":"0000000000000000000000000000000000000000000000000000000000000000","blockIndex":0}]`))
	f.Add([]byte(`{"topic":"\ud800"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := basm.Limits{MaxAdmitted: 16, MaxRange: 4, MaxJSONBytes: 4096, MaxTopicBytes: 32}
		anchor, err := basm.DecodeAnchorJSON(data, limits)
		if err == nil {
			require.NoError(t, anchor.Validate(limits))
		}
		refs, err := basm.DecodeAdmittedListJSON(context.Background(), data, 100, limits)
		if err == nil {
			_, err = basm.ValidateAdmittedList(context.Background(), refs, 100, limits)
			require.NoError(t, err)
		}
		from, to, err := basm.DecodeRangeJSON(data, limits)
		if err == nil {
			_, err = basm.ValidateRange(from, to, limits.MaxRange)
			require.NoError(t, err)
		}
	})
}
