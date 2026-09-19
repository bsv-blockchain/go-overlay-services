package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// admissionOperationID must not let distinct topic sets collide through
// unframed concatenation, and must treat a topic list's identity as the
// deduped set (matching AdmissionSemanticDigest/AdmissionIdentity), the same
// way it already treats the list as order-insensitive. See the review finding
// on pkg/core/engine/admission_submit.go (~line 343): AdmissionSemanticDigest
// already length-frames the same kind of fields; admissionOperationID must
// use the same discipline for its own hash.
func TestAdmissionOperationIDFramesFieldsToAvoidCollisions(t *testing.T) {
	// Concatenating "tm_foo" and "x" with no separator, after sorting, equals
	// the single topic "tm_foox": "tm_foo" + "x" == "tm_foox". Two distinct,
	// unrelated topic sets must not collapse to the same operation id, or the
	// second submit permanently digest-mismatches against the first's saved
	// receipt.
	twoTopics := admissionOperationID(AdmissionModeLive, txidFixture, []string{"tm_foo", "x"})
	oneTopic := admissionOperationID(AdmissionModeLive, txidFixture, []string{"tm_foox"})
	require.NotEqual(t, twoTopics, oneTopic, "distinct topic sets must not collide via unframed concatenation")
}

func TestAdmissionOperationIDTreatsDuplicateTopicsAsTheDedupedSet(t *testing.T) {
	deduped := admissionOperationID(AdmissionModeLive, txidFixture, []string{"tm_a", "tm_b"})
	withDuplicate := admissionOperationID(AdmissionModeLive, txidFixture, []string{"tm_a", "tm_a", "tm_b"})
	require.Equal(t, deduped, withDuplicate, "duplicated topics must resolve to the same operation id as the deduped identity, so a duplicate-free retry still matches the saved receipt")
}

func TestAdmissionOperationIDIsOrderInsensitive(t *testing.T) {
	ascending := admissionOperationID(AdmissionModeLive, txidFixture, []string{"tm_a", "tm_b"})
	descending := admissionOperationID(AdmissionModeLive, txidFixture, []string{"tm_b", "tm_a"})
	require.Equal(t, ascending, descending)
}

func TestAdmissionOperationIDStillDependsOnMode(t *testing.T) {
	// Not part of the collision/dedup bug: this documents the deliberate
	// current behavior (mode is part of the Go operation id, unlike the TS
	// overlay's overlayAdmissionOperationId, which intentionally drops mode).
	// That semantics choice is out of scope for this fix; only the framing and
	// dedup are corrected here.
	live := admissionOperationID(AdmissionModeLive, txidFixture, []string{"tm_a"})
	historical := admissionOperationID(AdmissionModeHistorical, txidFixture, []string{"tm_a"})
	require.NotEqual(t, live, historical)
}

const txidFixture = "1111111111111111111111111111111111111111111111111111111111111111"

func TestAdmissionOperationIDIsDomainSeparated(t *testing.T) {
	// The id is persisted as part of the operation key, so its byte format is pinned here:
	// SHA-256 over length-framed fields that start with a fixed domain tag, which keeps it
	// disjoint from AdmissionSemanticDigest (whose first framed field is the protocol name).
	sum := sha256.New()
	for _, field := range []string{"overlay-admission-operation-id-v1", "live", "aa", "tm_a", "tm_b"} {
		_, _ = sum.Write([]byte(strconv.Itoa(len(field)) + ":" + field))
	}

	require.Equal(t, hex.EncodeToString(sum.Sum(nil)), admissionOperationID(AdmissionModeLive, "aa", []string{"tm_b", "tm_a"}))
}
