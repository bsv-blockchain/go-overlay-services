package gasp

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// FuzzHexBytesUnmarshalJSON fuzzes the wire-facing hex decoder. HexBytes is
// what unmarshals rawTx/proof fields from GASP JSON messages, so it takes
// untrusted input directly. Carries over the malformed-hex corpus from the
// computeTxID harness, which owned hex decoding before HexBytes existed.
func FuzzHexBytesUnmarshalJSON(f *testing.F) {
	seeds := []string{
		`"01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffff0100f2052a01000000015100000000"`,
		`"deadbeef"`,
		`""`,
		`"010000000"`, // odd length
		`"0100000g"`,  // invalid character
		`"ZZZZZZZZ"`,
		`"0x01000000"`,
		`"AaBbCcDd"`,
		`null`,
		`123`,
		`{"a":1}`,
		"\"01000000\\u0000\"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var h HexBytes
		err := h.UnmarshalJSON(data)

		var s string
		isString := json.Unmarshal(data, &s) == nil
		switch {
		case string(data) == "null":
			// null clears and never errors
			if err != nil || h != nil {
				t.Errorf("UnmarshalJSON(null): err=%v value=%x", err, h)
			}
		case !isString:
			// non-string JSON must error
			if err == nil {
				t.Errorf("UnmarshalJSON(%s): no error for non-string input", data)
			}
		default:
			decoded, hexErr := hex.DecodeString(s)
			if hexErr != nil {
				if err == nil {
					t.Errorf("UnmarshalJSON(%s): no error for invalid hex", data)
				}
				return
			}
			// valid hex: must succeed and round-trip through MarshalJSON
			if err != nil {
				t.Errorf("UnmarshalJSON(%s): unexpected error %v", data, err)
				return
			}
			if !bytes.Equal(h, decoded) {
				t.Errorf("UnmarshalJSON(%s): got %x want %x", data, h, decoded)
			}
			out, mErr := h.MarshalJSON()
			if mErr != nil {
				t.Errorf("MarshalJSON(%x): %v", h, mErr)
				return
			}
			var h2 HexBytes
			if err := h2.UnmarshalJSON(out); err != nil || !bytes.Equal(h2, h) {
				t.Errorf("round-trip failed: %s -> %x -> %s", data, h, out)
			}
		}
	})
}
