package ports_test

import (
	"bytes"
	"testing"

	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/util"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
)

// opaqueOffChainValues is intentionally not JSON and not valid UTF-8.
var opaqueOffChainValues = []byte{0x00, 0xff, 0x01}

// HTTP tests stop at the submit provider, which is the engine boundary.
// Delivery into TopicManager.IdentifyAdmissibleOutputs is covered by
// TestEngine_Submit_PassesOffChainValuesToIdentifyAdmissibleOutputs.

func TestSubmitTransactionHandler_OffChainValuesHeaderNotExactlyTrue(t *testing.T) {
	body := []byte{0x10, 0x00, 0xff, 0x01, 0x22}
	headers := map[string]string{
		fiber.HeaderContentType: fiber.MIMEOctetStream,
		ports.XTopicsHeader:     "topic1,topic2",
		// Not exactly "true": the body must stay a raw BEEF.
		ports.XIncludesOffChainValuesHeader: "TRUE",
	}

	mock := postSubmit(t, headers, body)

	called := mock.CalledTaggedBEEF()
	require.Equal(t, body, called.Beef)
	require.Nil(t, called.OffChainValues)
}

func TestSubmitTransactionHandler_OffChainValuesHeaderAbsent(t *testing.T) {
	body := []byte("raw-beef-body")
	headers := map[string]string{
		fiber.HeaderContentType: fiber.MIMEOctetStream,
		ports.XTopicsHeader:     "topic1",
	}

	mock := postSubmit(t, headers, body)

	called := mock.CalledTaggedBEEF()
	require.Equal(t, body, called.Beef)
	require.Nil(t, called.OffChainValues)
}

func TestSubmitTransactionHandler_OffChainValuesFramed(t *testing.T) {
	beef := []byte("beef-bytes")
	body := frameOffChainSubmit(t, beef, opaqueOffChainValues)
	headers := map[string]string{
		fiber.HeaderContentType:             fiber.MIMEOctetStream,
		ports.XTopicsHeader:                 "topic1,topic2",
		ports.XIncludesOffChainValuesHeader: "true",
	}

	mock := postSubmit(t, headers, body)

	called := mock.CalledTaggedBEEF()
	require.Equal(t, beef, called.Beef)
	require.Equal(t, opaqueOffChainValues, called.OffChainValues)
	require.False(t, bytes.Equal(called.Beef, body), "framed body must not be passed through as the BEEF")
}

func TestSubmitTransactionHandler_OffChainValuesUsesBitcoinVarInt(t *testing.T) {
	// 253 forces the 0xfd uint16 form. A single-byte length prefix would not round-trip.
	beef := bytes.Repeat([]byte{0xab}, 253)
	body := frameOffChainSubmit(t, beef, opaqueOffChainValues)
	require.Equal(t, byte(0xfd), body[0])
	headers := map[string]string{
		fiber.HeaderContentType:             fiber.MIMEOctetStream,
		ports.XTopicsHeader:                 "topic1",
		ports.XIncludesOffChainValuesHeader: "true",
	}

	mock := postSubmit(t, headers, body)

	called := mock.CalledTaggedBEEF()
	require.Equal(t, beef, called.Beef)
	require.Equal(t, opaqueOffChainValues, called.OffChainValues)
}

func TestSubmitTransactionHandler_OffChainValuesEmptyTrailerIsNotNil(t *testing.T) {
	beef := []byte("beef-bytes")
	body := frameOffChainSubmit(t, beef, nil)
	headers := map[string]string{
		fiber.HeaderContentType:             fiber.MIMEOctetStream,
		ports.XTopicsHeader:                 "topic1",
		ports.XIncludesOffChainValuesHeader: "true",
	}

	mock := postSubmit(t, headers, body)

	called := mock.CalledTaggedBEEF()
	require.Equal(t, beef, called.Beef)
	require.NotNil(t, called.OffChainValues)
	require.Empty(t, called.OffChainValues)
}

func TestSubmitTransactionHandler_MalformedOffChainFrame(t *testing.T) {
	tests := map[string][]byte{
		"varint past end of body":          {0xff, 0x01},
		"truncated multi-byte varint":      {0xfd},
		"truncated 32-bit varint":          {0xfe, 0x01, 0x00},
		"beef length does not fit in body": {0x05, 0x01, 0x02},
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			expectations := testabilities.SubmitTransactionProviderMockExpectations{SubmitCall: false}
			mock := testabilities.NewSubmitTransactionProviderMock(t, expectations)
			stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithSubmitTransactionProvider(mock))
			fixture := server.NewTestFixture(t, server.WithEngine(stub))

			var actualResponse openapi.BadRequestResponse
			res, err := fixture.Client().
				R().
				SetHeaders(map[string]string{
					fiber.HeaderContentType:             fiber.MIMEOctetStream,
					ports.XTopicsHeader:                 "topic1",
					ports.XIncludesOffChainValuesHeader: "true",
				}).
				SetBody(body).
				SetError(&actualResponse).
				Post("/api/v1/submit")

			require.NoError(t, err)
			require.Equal(t, fiber.StatusBadRequest, res.StatusCode())
			expected := testabilities.NewTestOpenapiErrorResponse(t, ports.NewMalformedOffChainValuesFrameError("unused"))
			require.Equal(t, expected, actualResponse)
			stub.AssertProvidersState()
		})
	}
}

func postSubmit(t *testing.T, headers map[string]string, body []byte) *testabilities.SubmitTransactionProviderMock {
	t.Helper()

	expectations := testabilities.SubmitTransactionProviderMockExpectations{
		SubmitCall: true,
		STEAK: &overlay.Steak{
			"test": &overlay.AdmittanceInstructions{OutputsToAdmit: []uint32{1}},
		},
	}
	mock := testabilities.NewSubmitTransactionProviderMock(t, expectations)
	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithSubmitTransactionProvider(mock))
	fixture := server.NewTestFixture(t, server.WithEngine(stub))

	var actualResponse openapi.SubmitTransactionResponse
	res, err := fixture.Client().
		R().
		SetHeaders(headers).
		SetBody(body).
		SetResult(&actualResponse).
		Post("/api/v1/submit")

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	stub.AssertProvidersState()
	return mock
}

func frameOffChainSubmit(t *testing.T, beef, offChain []byte) []byte {
	t.Helper()
	writer := util.NewWriter()
	writer.WriteVarInt(uint64(len(beef)))
	writer.WriteBytes(beef)
	writer.WriteBytes(offChain)
	return writer.Buf
}
