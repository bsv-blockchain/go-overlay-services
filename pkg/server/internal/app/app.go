package app

import (
	"fmt"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/commands"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/queries"
)

// Commands aggregate all the supported commands by the overlay API.
type Commands struct {
	SubmitTransactionHandler      *commands.SubmitTransactionHandler
	SyncAdvertismentsHandler      *commands.SyncAdvertisementsHandler
	StartGASPSyncHandler          *commands.StartGASPSyncHandler
	RequestForeignGASPNodeHandler *commands.RequestForeignGASPNodeHandler
	RequestSyncResponseHandler    *commands.RequestSyncResponseHandler
	LookupQuestionHandler         *commands.LookupQuestionHandler
	ArcIngestHandler              *commands.ArcIngestHandler
}

// Queries aggregate all the supported queries by the overlay API.
type Queries struct {
	LookupServicesListHandler         *queries.LookupServicesListHandler
	TopicManagersListHandler          *queries.TopicManagersListHandler
	LookupServiceDocumentationHandler *queries.LookupServiceDocumentationHandler
	TopicManagerDocumentationHandler  *queries.TopicManagerDocumentationHandler
}

// Application aggregates queries and commands supported by the overlay API.
type Application struct {
	Commands *Commands
	Queries  *Queries
}

// New returns an instance of an Application with intialized commands and queries
// utilizing an implementation of OverlayEngineProvider. If the provided argument is nil, it triggers a panic.
func New(provider engine.OverlayEngineProvider) (*Application, error) {
	if provider == nil {
		return nil, fmt.Errorf("overlay engine provider is nil")
	}

	cmds, err := initCommands(provider)
	if err != nil {
		return nil, err
	}

	queries, err := initQueries(provider)
	if err != nil {
		return nil, err
	}

	return &Application{
		Commands: cmds,
		Queries:  queries,
	}, nil
}

func initCommands(provider engine.OverlayEngineProvider) (*Commands, error) {
	submitHandler, err := commands.NewSubmitTransactionCommandHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("SubmitTransactionHandler: %w", err)
	}

	syncAdsHandler, err := commands.NewSyncAdvertisementsCommandHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("SyncAdvertisementsHandler: %w", err)
	}

	startSyncHandler, err := commands.NewStartGASPSyncHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("StartGASPSyncHandler: %w", err)
	}

	requestGASPHandler, err := commands.NewRequestForeignGASPNodeHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("RequestForeignGASPNodeHandler: %w", err)
	}

	requestSyncRespHandler, err := commands.NewRequestSyncResponseHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("RequestSyncResponseHandler: %w", err)
	}

	lookupQuestionHandler, err := commands.NewLookupQuestionHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("LookupQuestionHandler: %w", err)
	}

	arcIngestHandler, err := commands.NewArcIngestHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("ArcIngestHandler: %w", err)
	}

	return &Commands{
		SubmitTransactionHandler:      submitHandler,
		SyncAdvertismentsHandler:      syncAdsHandler,
		StartGASPSyncHandler:          startSyncHandler,
		RequestForeignGASPNodeHandler: requestGASPHandler,
		RequestSyncResponseHandler:    requestSyncRespHandler,
		LookupQuestionHandler:         lookupQuestionHandler,
		ArcIngestHandler:              arcIngestHandler,
	}, nil
}

func initQueries(provider engine.OverlayEngineProvider) (*Queries, error) {
	topicDocHandler, err := queries.NewTopicManagerDocumentationHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("TopicManagerDocumentationHandler: %w", err)
	}

	topicListHandler, err := queries.NewTopicManagersListHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("TopicManagersListHandler: %w", err)
	}

	lookupServiceDocHandler, err := queries.NewLookupServiceDocumentationHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("LookupServiceDocumentationHandler: %w", err)
	}

	lookupServicesListHandler, err := queries.NewLookupServicesListHandler(provider)
	if err != nil {
		return nil, fmt.Errorf("LookupListHandler: %w", err)
	}

	return &Queries{
		TopicManagerDocumentationHandler:  topicDocHandler,
		TopicManagersListHandler:          topicListHandler,
		LookupServiceDocumentationHandler: lookupServiceDocHandler,
		LookupServicesListHandler:         lookupServicesListHandler,
	}, nil
}
