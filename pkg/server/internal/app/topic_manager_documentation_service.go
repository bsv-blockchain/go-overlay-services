package app

import (
	"context"
)

// TopicManagerDocumentationProvider defines the contract for retrieving documentation
// for a topic manager.
type TopicManagerDocumentationProvider interface {
	GetDocumentationForTopicManager(topicManagerName string) (string, error)
}

// TopicManagerDocumentationService provides functionality for retrieving topic manager documentation.
type TopicManagerDocumentationService struct {
	provider TopicManagerDocumentationProvider
}

// GetDocumentation retrieves documentation for a specific topic manager.
// Returns the documentation string on success, or an error if:
// - The topic manager name is empty (ErrorTypeIncorrectInput)
// - The provider fails to retrieve documentation (ErrorTypeProviderFailure)
func (s *TopicManagerDocumentationService) GetDocumentation(ctx context.Context, topicManagerName string) (string, error) {
	if topicManagerName == "" {
		return "", NewEmptyTopicManagerNameError()
	}

	documentation, err := s.provider.GetDocumentationForTopicManager(topicManagerName)
	if err != nil {
		return "", NewTopicManagerDocumentationProviderError(err)
	}

	return documentation, nil
}

// NewTopicManagerDocumentationService creates a new TopicManagerDocumentationService with the given provider.
// Panics if the provider is nil.
func NewTopicManagerDocumentationService(provider TopicManagerDocumentationProvider) *TopicManagerDocumentationService {
	if provider == nil {
		panic("topic manager documentation provider cannot be nil")
	}

	return &TopicManagerDocumentationService{
		provider: provider,
	}
}

// NewEmptyTopicManagerNameError returns an Error indicating that the topic manager name is empty,
// which is invalid input when retrieving documentation.
func NewEmptyTopicManagerNameError() Error {
	return Error{
		errorType: ErrorTypeIncorrectInput,
		err:       "topic manager name cannot be empty",
		slug:      "A valid topicManager must be provided to retrieve documentation.",
	}
}

// NewTopicManagerDocumentationProviderError returns an Error indicating that the configured provider
// failed to retrieve documentation for the topic manager.
func NewTopicManagerDocumentationProviderError(err error) Error {
	return Error{
		errorType: ErrorTypeProviderFailure,
		err:       "unable to retrieve documentation for topic manager",
		slug:      "Unable to retrieve documentation for topic manager due to an internal error. Please try again later or contact the support team.",
	}
}
