package mongodb

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

// EncodeUint64 converts an S01 canonical decimal integer to fixed-width decimal
// text. Under simple collation, its BSON string order is its unsigned numeric
// order, including values above both JavaScript's safe range and MaxInt64.
func EncodeUint64(value engine.StorageUint64) (string, error) {
	n, err := engine.ParseStorageUint64(value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%020d", n), nil
}

// DecodeUint64 validates fixed-width BSON text and returns S01 canonical text.
func DecodeUint64(value string) (engine.StorageUint64, error) {
	if len(value) != 20 {
		return "", engine.ErrInvalidStorageUint64
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return "", engine.ErrInvalidStorageUint64
		}
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return "", engine.ErrInvalidStorageUint64
	}
	return engine.StorageUint64(strconv.FormatUint(n, 10)), nil
}

func tupleID(fields ...string) string {
	h := sha256.New()
	for _, field := range fields {
		_, _ = fmt.Fprintf(h, "%d:", len(field))
		_, _ = h.Write([]byte(field))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validText(value string) bool {
	return len(value) > 0 && len(value) <= 1024 && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}

func validHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range len(value) {
		if (value[i] < '0' || value[i] > '9') && (value[i] < 'a' || value[i] > 'f') {
			return false
		}
	}
	return true
}

func validateScope(scope engine.StorageScope) error {
	if !validText(scope.Network) || !validText(scope.NodeID) || !validHash(scope.GenesisHash) {
		return ErrInvalidConfig
	}
	return nil
}
