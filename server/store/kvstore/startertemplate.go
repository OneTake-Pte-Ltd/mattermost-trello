package kvstore

import (
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"
)

// We expose our calls to the KVStore pluginapi methods through this interface for testability and stability.
// This allows us to better control which values are stored with which keys.

type Client struct {
	client *pluginapi.Client
}

func NewKVStore(client *pluginapi.Client) KVStore {
	return Client{
		client: client,
	}
}

// GetTemplateData retrieves a sample key-value pair from the KV store.
func (kv Client) GetTemplateData(userID string) (string, error) {
	var templateData string
	err := kv.client.KV.Get("template_key-"+userID, &templateData)
	if err != nil {
		return "", errors.Wrap(err, "failed to get template data")
	}
	return templateData, nil
}

// GetThreadCard returns the Trello card linked to the given Mattermost root post ID.
// Returns nil, nil when no card has been linked yet.
func (kv Client) GetThreadCard(rootPostID string) (*ThreadCard, error) {
	var card ThreadCard
	if err := kv.client.KV.Get("trello_thread_"+rootPostID, &card); err != nil {
		return nil, errors.Wrap(err, "failed to get thread card")
	}
	// pluginapi KV.Get leaves the struct at zero value when the key doesn't exist.
	if card.CardID == "" {
		return nil, nil
	}
	return &card, nil
}

// SetThreadCard links a Trello card to a Mattermost thread root post ID.
func (kv Client) SetThreadCard(rootPostID string, card *ThreadCard) error {
	if _, err := kv.client.KV.Set("trello_thread_"+rootPostID, card); err != nil {
		return errors.Wrap(err, "failed to set thread card")
	}
	return nil
}
