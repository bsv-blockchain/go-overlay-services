package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

type topicFixture struct {
	Name           string   `json:"name"`
	BEEFBase64     string   `json:"beefBase64"`
	BEEFSHA256     string   `json:"beefSha256"`
	Txid           string   `json:"txid"`
	OutputsToAdmit []uint32 `json:"outputsToAdmit"`
}

type topicCorpus struct {
	Fixtures          []topicFixture  `json:"fixtures"`
	SearchableProfile json.RawMessage `json:"searchableProfile"`
	OrderingProfiles  []struct {
		Name              string          `json:"name"`
		Certificate       json.RawMessage `json:"certificate"`
		UTF8Certificate   json.RawMessage `json:"utf8ComparatorCertificate"`
		TSPreimage        []byte          `json:"certificateToBinaryWithoutSignatureBase64"`
		UTF8Preimage      []byte          `json:"utf8ComparatorCertificatePreimageBase64"`
		TSVerify          bool            `json:"tsCertificateVerify"`
		UTF8VerifyUnderTS bool            `json:"utf8ComparatorCertificateVerifyUnderTS"`
	} `json:"orderingProfiles"`
}

func loadTopicCorpus(t testing.TB) topicCorpus {
	t.Helper()
	data, err := os.ReadFile("testdata/data/identity-topic-fixtures.json")
	require.NoError(t, err)
	var corpus topicCorpus
	require.NoError(t, json.Unmarshal(data, &corpus))
	return corpus
}

func readTopicFixture(t testing.TB, fixture topicFixture) (*transaction.Beef, *chainhash.Hash) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(fixture.BEEFBase64)
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	require.Equal(t, fixture.BEEFSHA256, hex.EncodeToString(digest[:]))
	beef, err := transaction.NewBeefFromBytes(raw)
	require.NoError(t, err)
	txid, err := chainhash.NewHashFromHex(fixture.Txid)
	require.NoError(t, err)
	require.Equal(t, fixture.Txid, beef.FindTransactionByHash(txid).TxID().String())
	return beef, txid
}

func TestTopicManagerTSFixtures(t *testing.T) {
	for _, fixture := range loadTopicCorpus(t).Fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			beef, txid := readTopicFixture(t, fixture)
			manager := NewTopicManager()
			result, err := manager.IdentifyAdmissibleOutputs(t.Context(), beef, txid, []uint32{0, 5})
			require.NoError(t, err)
			expected := fixture.OutputsToAdmit
			if fixture.Name == "mixed-valid-and-malformed-outputs" {
				// The TS-signed mixed case/accented certificate at index 5 has
				// a different binary field order. Keep the parity gap explicit.
				expected = []uint32{0, 6}
				_, projectionErr := ProjectOutput(t.Context(), transaction.Outpoint{Txid: *txid, Index: 5}, beef.FindTransactionByHash(txid).Outputs[5].LockingScript, DefaultAdmissionPolicy())
				require.ErrorIs(t, projectionErr, ErrCertificateVerification)
			}
			require.Equal(t, expected, result.OutputsToAdmit)
			require.Empty(t, result.CoinsToRetain)
			// Replay cannot alter admission decisions or mutate the BEEF.
			again, err := manager.IdentifyAdmissibleOutputs(t.Context(), beef, txid, nil)
			require.NoError(t, err)
			require.Equal(t, result, again)
			require.Equal(t, fixture.Txid, beef.FindTransactionByHash(txid).TxID().String())
		})
	}
}

func TestProjectOutputSeparatesInvalidOutputs(t *testing.T) {
	fixture := loadTopicCorpus(t).Fixtures[1]
	beef, txid := readTopicFixture(t, fixture)
	outputs := beef.FindTransactionByHash(txid).Outputs
	for _, index := range []uint32{1, 2, 3, 4} {
		_, err := ProjectOutput(t.Context(), transaction.Outpoint{Txid: *txid, Index: index}, outputs[index].LockingScript, DefaultAdmissionPolicy())
		require.Error(t, err, "malformed output %d", index)
	}
	valid, err := ProjectOutput(t.Context(), transaction.Outpoint{Txid: *txid}, outputs[0].LockingScript, DefaultAdmissionPolicy())
	require.NoError(t, err)
	require.Equal(t, map[string]string{"name": "Alice"}, valid.Certificate.Fields)
	require.Equal(t, "Alice", valid.SearchableAttributes)
}

func TestCertificateSerializationProfiles(t *testing.T) {
	for _, profile := range loadTopicCorpus(t).OrderingProfiles {
		t.Run(profile.Name, func(t *testing.T) {
			var envelope certificateEnvelope
			require.NoError(t, json.Unmarshal(profile.Certificate, &envelope))
			certificate, err := envelope.certificate(DefaultAdmissionPolicy())
			require.NoError(t, err)
			preimage, err := certificate.ToBinary(false)
			require.NoError(t, err)
			require.Equal(t, profile.UTF8Preimage, preimage, "Go must reproduce the exact UTF8 byte-order preimage")
			require.True(t, profile.TSVerify, "actual TS certificate verification")
			if profile.UTF8VerifyUnderTS {
				require.Equal(t, profile.TSPreimage, preimage)
				require.NoError(t, certificate.Verify(t.Context()))
			} else {
				require.NotEqual(t, profile.TSPreimage, preimage)
				require.Error(t, certificate.Verify(t.Context()), "documented unsupported TS serialization")
			}
			var utf8Envelope certificateEnvelope
			require.NoError(t, json.Unmarshal(profile.UTF8Certificate, &utf8Envelope))
			utf8Certificate, err := utf8Envelope.certificate(DefaultAdmissionPolicy())
			require.NoError(t, err)
			require.NoError(t, utf8Certificate.Verify(t.Context()), "actual TS wallet signed the byte-order preimage")
		})
	}
}

func TestTopicManagerRejectsMalformedSelectionAndBudgets(t *testing.T) {
	fixture := loadTopicCorpus(t).Fixtures[0]
	beef, txid := readTopicFixture(t, fixture)
	manager := NewTopicManager()
	for _, test := range []struct {
		name string
		beef *transaction.Beef
		txid *chainhash.Hash
	}{
		{name: "nil BEEF", txid: txid},
		{name: "nil txid", beef: beef},
		{name: "missing transaction", beef: transaction.NewBeef(), txid: txid},
		{name: "nil map entry", beef: &transaction.Beef{Transactions: map[chainhash.Hash]*transaction.BeefTx{*txid: nil}}, txid: txid},
		{name: "txid only", beef: &transaction.Beef{Transactions: map[chainhash.Hash]*transaction.BeefTx{*txid: {DataFormat: transaction.TxIDOnly}}}, txid: txid},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := manager.IdentifyAdmissibleOutputs(t.Context(), test.beef, test.txid, nil)
			require.ErrorIs(t, err, ErrInvalidTransaction)
			require.Empty(t, result.OutputsToAdmit)
		})
	}
	wrongID := chainhash.HashH([]byte("wrong map identity"))
	beef.Transactions[wrongID] = beef.Transactions[*txid]
	_, err := manager.IdentifyAdmissibleOutputs(t.Context(), beef, &wrongID, nil)
	require.ErrorIs(t, err, ErrInvalidTransaction)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = manager.IdentifyAdmissibleOutputs(ctx, beef, txid, nil)
	require.ErrorIs(t, err, context.Canceled)
	policy := DefaultAdmissionPolicy()
	policy.MaxScriptBytes = 1
	bounded, err := NewTopicManagerWithPolicy(policy)
	require.NoError(t, err)
	result, err := bounded.IdentifyAdmissibleOutputs(t.Context(), beef, txid, nil)
	require.NoError(t, err)
	require.Empty(t, result.OutputsToAdmit)
	policy.MaxTotalScriptBytes = 1
	bounded, err = NewTopicManagerWithPolicy(policy)
	require.NoError(t, err)
	_, err = bounded.IdentifyAdmissibleOutputs(t.Context(), beef, txid, nil)
	require.ErrorIs(t, err, ErrAdmissionBudget)
	_, err = NewTopicManagerWithPolicy(AdmissionPolicy{})
	require.ErrorIs(t, err, ErrInvalidPolicy)
}

func TestTopicManagerMetadataAndDependencies(t *testing.T) {
	manager := NewTopicManager()
	require.NotEmpty(t, manager.GetDocumentation())
	require.Equal(t, "Identity Topic Manager", manager.GetMetaData().Name)
	manager.GetMetaData().Name = "mutated"
	require.Equal(t, "Identity Topic Manager", manager.GetMetaData().Name)
	inputs, err := manager.IdentifyNeededInputs(t.Context(), nil, nil)
	require.NoError(t, err)
	require.Empty(t, inputs)
}

func FuzzProjectOutput(f *testing.F) {
	fixture := loadTopicCorpus(f).Fixtures[0]
	beef, txid := readTopicFixture(f, fixture)
	f.Add([]byte(*beef.FindTransactionByHash(txid).Outputs[0].LockingScript))
	f.Add([]byte{})
	f.Add([]byte{0x4c, 0xff})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > DefaultAdmissionPolicy().MaxScriptBytes {
			t.Skip()
		}
		lockingScript := script.Script(raw)
		_, _ = ProjectOutput(t.Context(), transaction.Outpoint{}, &lockingScript, DefaultAdmissionPolicy())
	})
}
