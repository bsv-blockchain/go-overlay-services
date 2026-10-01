package ports

import (
	"bytes"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/util"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
)

// XIncludesOffChainValuesHeader is the optional POST /submit header that
// frames off-chain values after the BEEF payload.
//
// Only the exact value "true" opts in. Any other value, including a different
// case or surrounding whitespace, means the body is a raw BEEF and off-chain
// values are absent.
const XIncludesOffChainValuesHeader = "x-includes-off-chain-values"

const offChainValuesHeaderTrue = "true"

const malformedOffChainValuesFrameSlug = "The request body is not a valid off-chain values frame. " +
	"Expected a Bitcoin varint beef length, the BEEF bytes, and optional trailing off-chain value bytes."

// includesOffChainValues reports whether the header value requests the
// varint-framed submit body. Comparison is exact and case-sensitive.
func includesOffChainValues(headerValue string) bool {
	return headerValue == offChainValuesHeaderTrue
}

// splitSubmitBody separates a POST /submit body into BEEF bytes and optional
// off-chain values.
//
// When includesOffChain is false, beef is a copy of body and offChainValues
// is nil. When it is true, body must be VarInt(beefByteLength) || beefBytes ||
// offChainValueBytes, using the Bitcoin varint encoding from go-sdk/util.
// Trailing bytes are returned unchanged and are not decoded as JSON or text.
// A present header with no trailing bytes yields a non-nil empty slice so
// callers can distinguish "no off-chain values" (nil) from an empty payload.
//
// A varint that runs past the end of body, or a beef length that does not fit
// in the remaining bytes, returns an incorrect-input error (HTTP 400).
func splitSubmitBody(body []byte, includesOffChain bool) (beef, offChainValues []byte, err error) {
	if !includesOffChain {
		return bytes.Clone(body), nil, nil
	}

	reader := util.NewReader(body)
	length, readErr := reader.ReadVarInt()
	if readErr != nil {
		return nil, nil, NewMalformedOffChainValuesFrameError(readErr.Error())
	}
	if reader.Pos < 0 || reader.Pos > len(body) {
		return nil, nil, NewMalformedOffChainValuesFrameError("varint read past end of body")
	}
	remaining := len(body) - reader.Pos
	// remaining is nonnegative. Reject a beef length that does not fit before converting it to int.
	if length > uint64(remaining) { //nolint:gosec // G115 -- remaining is nonnegative after the position check above
		return nil, nil, NewMalformedOffChainValuesFrameError(
			fmt.Sprintf("beef length %d does not fit in %d remaining bytes", length, remaining),
		)
	}

	rawBeef, readErr := reader.ReadBytes(int(length)) //nolint:gosec // G115 -- length is bounded by remaining, which fits in int
	if readErr != nil {
		return nil, nil, NewMalformedOffChainValuesFrameError(readErr.Error())
	}

	offChain := bytes.Clone(reader.ReadRemaining())
	if offChain == nil {
		// Header was set, so an empty trailer is an empty payload, not "absent".
		offChain = []byte{}
	}
	return bytes.Clone(rawBeef), offChain, nil
}

// NewMalformedOffChainValuesFrameError reports that a submit body with
// x-includes-off-chain-values: true is not a valid Bitcoin varint frame.
func NewMalformedOffChainValuesFrameError(detail string) app.Error {
	return app.NewIncorrectInputError(
		"malformed off-chain values frame: "+detail,
		malformedOffChainValuesFrameSlug,
	)
}
