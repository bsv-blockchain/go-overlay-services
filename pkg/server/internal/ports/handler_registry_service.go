package ports

import (
	"github.com/gofiber/fiber/v2"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/decorators"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
)

// HandlerRegistryService defines the main point for registering HTTP handler dependencies.
// It acts as a central registry for mapping API endpoints to their handler implementations.
type HandlerRegistryService struct {
	lookupDocumentation       *LookupProviderDocumentationHandler
	startGASPSync             *StartGASPSyncHandler
	topicManagerDocumentation *TopicManagerDocumentationHandler
	submitTransaction         *SubmitTransactionHandler
	syncAdvertisements        *SyncAdvertisementsHandler
	requestForeignGASPNode    *RequestForeignGASPNodeHandler
	requestSyncResponse       *RequestSyncResponseHandler
	metadataHandler           *MetadataHandler
	lookupQuestion            *LookupQuestionHandler
	arcIngest                 decorators.Handler
	basmRead                  *BASMReadHandler
}

// SetBASMProvider configures the optional BASM read routes after registry construction.
// Existing registry construction remains compatible with engines that do not support BASM.
func (h *HandlerRegistryService) SetBASMProvider(provider engine.BASMProvider, limits basm.ReadLimits) {
	h.basmRead = NewBASMReadHandler(provider, limits)
}

// ArcIngest implements openapi.ServerInterface.
func (h *HandlerRegistryService) ArcIngest(c *fiber.Ctx) error {
	return h.arcIngest.Handle(c)
}

// LookupQuestion implements openapi.ServerInterface.
func (h *HandlerRegistryService) LookupQuestion(c *fiber.Ctx) error {
	return h.lookupQuestion.Handle(c)
}

// ListLookupServiceProviders method delegates the request to the configured lookup list handler.
func (h *HandlerRegistryService) ListLookupServiceProviders(c *fiber.Ctx) error {
	return h.metadataHandler.Handle(c, app.LookupsMetadataServiceMetadataType)
}

// AdvertisementsSync method delegates the request to the configured sync advertisements handler.
func (h *HandlerRegistryService) AdvertisementsSync(c *fiber.Ctx) error {
	return h.syncAdvertisements.Handle(c)
}

// GetLookupServiceProviderDocumentation method delegates the request to the configured lookup service provider documentation handler.
func (h *HandlerRegistryService) GetLookupServiceProviderDocumentation(c *fiber.Ctx, params openapi.GetLookupServiceProviderDocumentationParams) error {
	return h.lookupDocumentation.Handle(c, params)
}

// GetTopicManagerDocumentation method delegates the request to the configured topic manager documentation handler.
func (h *HandlerRegistryService) GetTopicManagerDocumentation(c *fiber.Ctx, params openapi.GetTopicManagerDocumentationParams) error {
	return h.topicManagerDocumentation.Handle(c, params)
}

// SubmitTransaction method delegates the request to the configured submit transaction handler.
func (h *HandlerRegistryService) SubmitTransaction(c *fiber.Ctx, params openapi.SubmitTransactionParams) error {
	return h.submitTransaction.Handle(c, params)
}

// ListTopicManagers method delegates the request to the configured topic managers list handler.
func (h *HandlerRegistryService) ListTopicManagers(c *fiber.Ctx) error {
	return h.metadataHandler.Handle(c, app.TopicManagersServiceMetadataType)
}

// StartGASPSync method delegates the request to the configured start GASP sync handler.
func (h *HandlerRegistryService) StartGASPSync(c *fiber.Ctx) error {
	return h.startGASPSync.Handle(c)
}

// RequestForeignGASPNode method delegates the request to the configured request foreign GASP node handler.
func (h *HandlerRegistryService) RequestForeignGASPNode(c *fiber.Ctx, params openapi.RequestForeignGASPNodeParams) error {
	return h.requestForeignGASPNode.Handle(c, params)
}

// RequestSyncResponse method delegates the request to the configured request sync response handler.
func (h *HandlerRegistryService) RequestSyncResponse(c *fiber.Ctx, params openapi.RequestSyncResponseParams) error {
	return h.requestSyncResponse.Handle(c, params)
}

// RequestTopicAnchorTip implements the public BASM tip route.
func (h *HandlerRegistryService) RequestTopicAnchorTip(c *fiber.Ctx, _ openapi.RequestTopicAnchorTipParams) error {
	return h.basmRead.Handle(c, "tip", true)
}

// RequestTopicAnchorRange implements the public BASM range route.
func (h *HandlerRegistryService) RequestTopicAnchorRange(c *fiber.Ctx, _ openapi.RequestTopicAnchorRangeParams) error {
	return h.basmRead.Handle(c, "range", true)
}

// RequestAdmittedList implements the public BASM admitted-list route.
func (h *HandlerRegistryService) RequestAdmittedList(c *fiber.Ctx, _ openapi.RequestAdmittedListParams) error {
	return h.basmRead.Handle(c, "list", true)
}

// RequestCompoundMerklePath implements the public BASM proof route.
func (h *HandlerRegistryService) RequestCompoundMerklePath(c *fiber.Ctx, _ openapi.RequestCompoundMerklePathParams) error {
	return h.basmRead.Handle(c, "proof", true)
}

// RequestRawTransactions implements the public BASM raw transaction route.
func (h *HandlerRegistryService) RequestRawTransactions(c *fiber.Ctx) error {
	return h.basmRead.Handle(c, "raw", false)
}

// NewHandlerRegistryService creates and returns a new HandlerRegistryService instance.
// It initializes all handler implementations with their required dependencies.
func NewHandlerRegistryService(provider engine.OverlayEngineProvider, cfg *decorators.ARCAuthorizationDecoratorConfig) *HandlerRegistryService {
	return &HandlerRegistryService{
		lookupDocumentation: NewLookupProviderDocumentationHandler(provider),
		startGASPSync:       NewStartGASPSyncHandler(provider),
		arcIngest:           decorators.NewArcAuthorizationDecorator(NewARCIngestHandler(provider), cfg),
		metadataHandler: NewMetadataHandler(
			app.NewMetadataService(
				app.NewLookupListService(provider),
				app.NewTopicManagersMetadataService(provider),
			),
		),
		lookupQuestion:            NewLookupQuestionHandler(provider),
		topicManagerDocumentation: NewTopicManagerDocumentationHandler(provider),
		submitTransaction:         NewSubmitTransactionHandler(provider),
		syncAdvertisements:        NewSyncAdvertisementsHandler(provider),
		requestForeignGASPNode:    NewRequestForeignGASPNodeHandler(provider),
		requestSyncResponse:       NewRequestSyncResponseHandler(provider),
		basmRead:                  NewBASMReadHandler(nil, basm.DefaultReadLimits()),
	}
}
