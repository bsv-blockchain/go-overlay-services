package identity

import (
	"context"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// Certificate is the public certificate data indexed by an identity projection.
// Fields contains publicly decrypted attributes, including profilePhoto and
// icon. SearchableAttributes on the enclosing Record excludes those two fields.
type Certificate struct {
	Type               string
	SerialNumber       string
	Subject            string
	Certifier          string
	RevocationOutpoint string
	Fields             map[string]string
}

// Record is one identity certificate projection associated with an unspent
// transaction output.
type Record struct {
	Outpoint             transaction.Outpoint
	Certificate          Certificate
	SearchableAttributes string
}

// Projection persists and queries identity certificate records.
//
// Upsert must use Outpoint as its unique identity, so replaying an admission
// replaces the existing projection instead of creating a duplicate. Delete is
// idempotent: removing an already absent outpoint succeeds. Implementations
// must durably acknowledge successful calls; replay coordination, tombstones,
// and outbox delivery belong to the surrounding ingestion workflow. This
// package intentionally provides no in-memory production adapter.
type Projection interface {
	Upsert(ctx context.Context, record Record) error
	Delete(ctx context.Context, outpoint transaction.Outpoint) error
	Find(ctx context.Context, query Query) ([]transaction.Outpoint, error)
}
