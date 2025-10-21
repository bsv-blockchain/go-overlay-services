// Package advertiser provides interfaces and types for managing blockchain overlay advertisements.
package advertiser

import (
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/script"
)

// Advertisement represents a single overlay service advertisement with protocol details and transaction data.
type Advertisement struct {
	Protocol       overlay.Protocol
	IdentityKey    string
	Domain         string
	TopicOrService string
	Beef           []byte
	OutputIndex    uint32
}

// AdvertisementData contains the protocol and topic/service information needed to create an advertisement.
type AdvertisementData struct {
	Protocol           overlay.Protocol
	TopicOrServiceName string
}

// Advertiser provides methods for creating, finding, revoking, and parsing overlay service advertisements.
type Advertiser interface {
	CreateAdvertisements(adsData []*AdvertisementData) (overlay.TaggedBEEF, error)
	FindAllAdvertisements(protocol overlay.Protocol) ([]*Advertisement, error)
	RevokeAdvertisements(advertisements []*Advertisement) (overlay.TaggedBEEF, error)
	ParseAdvertisement(outputScript *script.Script) (*Advertisement, error)
}
