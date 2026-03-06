package kvstore

// ThreadCard holds the Trello card associated with a Mattermost thread.
type ThreadCard struct {
	CardID      string `json:"cardId"`
	CardURL     string `json:"cardUrl"`
	BotUsername string `json:"botUsername"`
}

// KVStore abstracts the plugin's persistent storage needs.
type KVStore interface {
	GetTemplateData(userID string) (string, error)

	// GetThreadCard returns the Trello card linked to the given Mattermost root post ID.
	// Returns nil, nil when no card has been linked yet.
	GetThreadCard(rootPostID string) (*ThreadCard, error)

	// SetThreadCard links a Trello card to a Mattermost thread root post ID.
	SetThreadCard(rootPostID string, card *ThreadCard) error
}
