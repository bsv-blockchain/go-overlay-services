package basm

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnchorBinaryRoundTripPreservesInternalHashBytes(t *testing.T) {
	anchor := TopicBlockAnchor{
		Topic:         "tm_test",
		BlockHeight:   850000,
		BlockHash:     Hash{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31},
		BASMRoot:      Hash{31, 30, 29, 28, 27, 26, 25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
		AdmittedCount: 253,
	}

	encoded, err := EncodeAnchorBinary(anchor, DefaultLimits())
	require.NoError(t, err)
	// Independently serialized with Python's int.to_bytes and byte concatenation.
	assert.Equal(t, "07746d5f7465737450f80c00000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f1f1e1d1c1b1a191817161514131211100f0e0d0c0b0a09080706050403020100fdfd00", hex.EncodeToString(encoded))
	require.EqualValues(t, len(anchor.Topic), encoded[0])
	require.Equal(t, anchor.BlockHash[:], encoded[1+len(anchor.Topic)+4:1+len(anchor.Topic)+4+hashSize])
	require.Equal(t, []byte{0xfd, 0xfd, 0}, encoded[len(encoded)-3:])

	decoded, err := DecodeAnchorBinary(encoded, DefaultLimits())
	require.NoError(t, err)
	assert.Equal(t, anchor, decoded)
}

func TestDecodeAnchorBinaryRejectsMalformedEncodings(t *testing.T) {
	valid, err := EncodeAnchorBinary(TopicBlockAnchor{Topic: "a", AdmittedCount: 0}, DefaultLimits())
	require.NoError(t, err)

	tests := []struct {
		name string
		data []byte
	}{
		{name: "noncanonical topic length", data: append([]byte{0xfd, 1, 0}, valid[1:]...)},
		{name: "truncated", data: valid[:len(valid)-1]},
		{name: "trailing bytes", data: append(append([]byte(nil), valid...), 0)},
		{name: "invalid UTF8", data: append([]byte{1, 0xff, 0, 0, 0, 0}, make([]byte, 64)...)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, decodeErr := DecodeAnchorBinary(test.data, DefaultLimits())
			require.Error(t, decodeErr)
		})
	}
}

func TestParseBoundedMerklePathAcceptsOfficialBRC74Vector(t *testing.T) {
	// BRC-74's published vector has a synthetic explanatory header in the
	// specification; this test verifies only the BUMP binary and root.
	data, err := hex.DecodeString("fe8a6a0c000c04fde80b0011774f01d26412f0d16ea3f0447be0b5ebec67b0782e321a7a01cbdf7f734e30fde90b02004e53753e3fe4667073063a17987292cfdea278824e9888e52180581d7188d8fdea0b025e441996fc53f0191d649e68a200e752fb5f39e0d5617083408fa179ddc5c998fdeb0b0102fdf405000671394f72237d08a4277f4435e5b6edf7adc272f25effef27cdfe805ce71a81fdf50500262bccabec6c4af3ed00cc7a7414edea9c5efa92fb8623dd6160a001450a528201fdfb020101fd7c010093b3efca9b77ddec914f8effac691ecb54e2c81d0ab81cbc4c4b93befe418e8501bf01015e005881826eb6973c54003a02118fe270f03d46d02681c8bc71cd44c613e86302f8012e00e07a2bb8bb75e5accff266022e1e5e6e7b4d6d943a04faadcf2ab4a22f796ff30116008120cafa17309c0bb0e0ffce835286b3a2dcae48e4497ae2d2b7ced4f051507d010a00502e59ac92f46543c23006bff855d96f5e648043f0fb87a7a5949e6a9bebae430104001ccd9f8f64f4d0489b30cc815351cf425e0e78ad79a589350e4341ac165dbe45010301010000af8764ce7e1cc132ab5ed2229a005c87201c9a5ee15c0f91dd53eff31ab30cd4")
	require.NoError(t, err)

	path, err := ParseBoundedMerklePath(data, maxBytesForTest(data))
	require.NoError(t, err)
	require.Equal(t, uint32(813706), path.BlockHeight)
	require.Len(t, path.Path, 12)
	txid, err := chainhash.NewHashFromHex("304e737fdfcb017a1a322e78b067ecebb5e07b44f0a36ed1f01264d2014f7711")
	require.NoError(t, err)
	root, err := path.ComputeRoot(txid)
	require.NoError(t, err)
	assert.Equal(t, "57aab6e6fb1b697174ffb64e062c4728f2ffd33ddcfa02a43b64d8cd29b483b4", root.String())
}

func TestParseBoundedMerklePathRejectsUntrustedCountsFlagsOffsetsAndTrailingData(t *testing.T) {
	valid := append([]byte{1, 1, 1, 0, 0}, bytes.Repeat([]byte{1}, hashSize)...)
	unsafeOffset := append([]byte{1, 2, 1, 0, 0}, bytes.Repeat([]byte{1}, hashSize)...)
	unsafeOffset = append(unsafeOffset, 1, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1)
	duplicateOffset := append([]byte{1, 1, 2, 0, 0}, bytes.Repeat([]byte{1}, hashSize)...)
	duplicateOffset = append(duplicateOffset, 0, 0)
	duplicateOffset = append(duplicateOffset, bytes.Repeat([]byte{2}, hashSize)...)

	tests := []struct {
		name string
		data []byte
	}{
		{name: "noncanonical height", data: append([]byte{0xfd, 1, 0}, valid[1:]...)},
		{name: "huge count tiny payload", data: []byte{1, 1, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{name: "truncated hash", data: valid[:len(valid)-1]},
		{name: "unknown flags", data: append(append([]byte{1, 1, 1, 0, 4}, bytes.Repeat([]byte{1}, hashSize)...), 0)},
		{name: "duplicate and txid flags", data: []byte{1, 1, 1, 0, 3}},
		{name: "duplicate on left", data: append([]byte{1, 1, 2, 0, 1, 1, 0}, bytes.Repeat([]byte{1}, hashSize)...)},
		{name: "duplicate offsets", data: duplicateOffset},
		{name: "unsafe offset", data: unsafeOffset},
		{name: "trailing bytes", data: append(append([]byte(nil), valid...), 0)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, parseErr := ParseBoundedMerklePath(test.data, 1024)
			require.Error(t, parseErr)
		})
	}
}

func TestParseBoundedMerklePathAllowsPrunedInternalLevel(t *testing.T) {
	data := []byte{1, 2, 4}
	for offset := byte(0); offset < 4; offset++ {
		data = append(data, offset, 2)
		data = append(data, bytes.Repeat([]byte{offset + 1}, hashSize)...)
	}
	data = append(data, 0) // Both level-one parents can be derived from leaves.
	path, err := ParseBoundedMerklePath(data, 1024)
	require.NoError(t, err)
	require.Len(t, path.Path, 2)
	assert.Empty(t, path.Path[1])
	_, err = ParseBoundedMerklePath([]byte{1, 2, 0, 0}, 1024)
	require.Error(t, err)
}

func TestRawTransactionIDAcceptsGenesisTransaction(t *testing.T) {
	data, err := hex.DecodeString("01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff4d04ffff001d0104455468652054696d65732030332f4a616e2f32303039204368616e63656c6c6f72206f6e206272696e6b206f66207365636f6e64206261696c6f757420666f722062616e6b73ffffffff0100f2052a01000000434104678afdb0fe5548271967f1a67130b7105cd6a828e03909a67962e0ea1f61deb649f6bc3f4cef38c4f35504e51ec112de5c384df7ba0b8d578a4c702b6bf11d5fac00000000")
	require.NoError(t, err)

	id, err := RawTransactionID(data, maxBytesForTest(data))
	require.NoError(t, err)
	assert.Equal(t, "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b", id.String())
}

func TestRawTransactionIDRejectsMalformedAndExtendedData(t *testing.T) {
	valid, err := hex.DecodeString("01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffff010000000000000000000000000000")
	require.NoError(t, err)
	tests := []struct {
		name string
		data []byte
	}{
		{name: "noncanonical input count", data: append([]byte{1, 0, 0, 0, 0xfd, 1, 0}, valid[5:]...)},
		{name: "huge input count tiny payload", data: []byte{1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{name: "declared script exceeds data", data: append(append([]byte(nil), valid[:41]...), append([]byte{1}, valid[42:]...)...)},
		{name: "trailing bytes", data: append(append([]byte(nil), valid...), 0)},
		{name: "extended marker", data: []byte{1, 0, 0, 0, 0, 0, 0, 0, 0xef, 0, 0, 0, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, idErr := RawTransactionID(test.data, 1024)
			require.Error(t, idErr)
		})
	}
}

func TestRawTransactionIDAllowsLargeValidOutputScript(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a 17 MiB standard transaction")
	}
	script := bytes.Repeat([]byte{0x51}, 17<<20)
	data := make([]byte, 0, len(script)+64)
	data = append(data, 1, 0, 0, 0, 1)
	data = append(data, make([]byte, 32)...)
	data = append(data, 0xff, 0xff, 0xff, 0xff, 0)
	data = append(data, 0xff, 0xff, 0xff, 0xff, 1)
	data = append(data, make([]byte, 8)...)
	data = appendCompactSize(data, uint64(len(script)))
	data = append(data, script...)
	data = append(data, 0, 0, 0, 0)

	id, err := RawTransactionID(data, maxBytesForTest(data))
	require.NoError(t, err)
	assert.NotEqual(t, Hash{}, id)
}

func maxBytesForTest(data []byte) uint32 {
	return uint32(len(data)) //nolint:gosec // each inline fixture is bounded well below uint32
}

func FuzzBoundedWireDecoders(f *testing.F) {
	f.Add([]byte{1, 1, 0})
	f.Add(append([]byte{0, 1, 1, 0, 2}, make([]byte, hashSize)...))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	anchor, err := EncodeAnchorBinary(TopicBlockAnchor{Topic: "tm_fuzz"}, DefaultLimits())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(anchor)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		_, _ = DecodeAnchorBinary(data, DefaultLimits())
		_, _ = ParseBoundedMerklePath(data, 4096)
		_, _ = RawTransactionID(data, 4096)
	})
}
