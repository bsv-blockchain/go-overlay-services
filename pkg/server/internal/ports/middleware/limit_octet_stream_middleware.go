package middleware

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/gofiber/fiber/v2"
)

// ReadBodyLimit1GB defines the maximum allowed bytes read size (in bytes).
// This limit is set to 1GB to protect against excessively large payloads.
const ReadBodyLimit1GB = 1000 * 1024 * 1024 // 1,000 MB

// chunkSize defines the size of each chunk (in bytes) read from the input stream.
// Reading in smaller chunks helps control memory usage during large reads.
const chunkSize = 64 * 1024 // 64KB

// LimitedBytesReader is a utility for safely reading bytes with an enforced size limit.
// It is typically used to prevent reading more than a configured number of bytes
// from an incoming payload (e.g., request body).
type limitedBytesReader struct {
	// Bytes is the source data to be read.
	bytes []byte

	// ReadLimit defines the maximum number of bytes allowed to be read from Bytes.
	// If this limit is exceeded during reading, an error is returned.
	readLimit int64
}

// Read reads from the underlying byte slice up to the specified ReadLimit.
// It processes the input in 64KB chunks and returns the entire read data as a byte slice.
//
// If more than ReadLimit bytes are encountered, the function returns BodySizeLimitExceededError.
// If the byte slice is empty, it returns EmptyRequestBodyError.
// If an I/O or buffering error occurs during the read, it returns BodyReadError.
func (l *limitedBytesReader) Read() ([]byte, error) {
	if len(l.bytes) == 0 {
		return nil, NewEmptyRequestBodyError()
	}

	reader := io.LimitReader(bytes.NewBuffer(l.bytes), l.readLimit+1)
	buff := bytes.NewBuffer(nil)
	bb := make([]byte, chunkSize)
	var read int64

	for {
		n, err := reader.Read(bb)
		if n > 0 {
			read += int64(n)
			if read > l.readLimit {
				return nil, NewBodySizeLimitExceededError(l.readLimit)
			}
			_, writeErr := buff.Write(bb[:n])
			if writeErr != nil {
				return nil, NewBodyReadError(writeErr)
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, NewBodyReadError(err)
		}
	}
	return buff.Bytes(), nil
}

// LimitOctetStreamBodyMiddleware is a Fiber middleware that limits the size of incoming
// request bodies with the Content-Type: application/octet-stream. It reads the body in chunks
// and ensures that the body does not exceed the specified size limit.
func LimitOctetStreamBodyMiddleware(octetStreamLimit int64) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !c.Is(fiber.MIMEOctetStream) {
			return c.Next()
		}

		reader := limitedBytesReader{
			bytes:     c.Body(),
			readLimit: octetStreamLimit,
		}

		bytes, err := reader.Read()
		if err != nil {
			return err
		}

		c.Context().SetBody(bytes)
		return c.Next()
	}
}

// NewBodySizeLimitExceededError returns an error indicating that the request body exceeds the allowed maximum size.
func NewBodySizeLimitExceededError(limit int64) app.Error {
	msg := fmt.Sprintf("The submitted octet-stream exceeds the maximum allowed size: %d bytes.", limit)
	return app.NewIncorrectInputError(msg, msg)
}

// NewUnsupportedContentTypeError returns an error indicating that the submitted content type is not supported.
// It includes the expected content type in the message.
func NewUnsupportedContentTypeError(expected string) app.Error {
	msg := fmt.Sprintf("Unsupported content type. Expected: %s.", expected)
	return app.NewIncorrectInputError(msg, msg)
}

// NewBodyReadError returns an error indicating that the request body could not be read or processed.
// It wraps the original error with a user-facing message.
func NewBodyReadError(err error) app.Error {
	return app.NewRawDataProcessingError(
		err.Error(),
		"Unable to process request with content type octet-stream. Please verify the request content and try again later.",
	)
}

// NewEmptyRequestBodyError returns an error indicating that the request body is empty, which is not allowed.
func NewEmptyRequestBodyError() app.Error {
	const msg = "Unable to process request with content type octet-stream. The request body is empty."
	return app.NewIncorrectInputError(msg, msg)
}
