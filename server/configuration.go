package main

import (
	"encoding/json"
	"reflect"

	"github.com/pkg/errors"
)

// BotConfig defines the mapping between a Mattermost bot account and a Trello board/list.
type BotConfig struct {
	BotUsername    string `json:"botUsername"`
	BotDisplayName string `json:"botDisplayName"`
	TrelloAPIKey   string `json:"trelloApiKey"`
	TrelloAPIToken string `json:"trelloApiToken"`
	TrelloBoardID  string `json:"trelloBoardId"`
	TrelloListID   string `json:"trelloListId"`
	// BotContext is optional free-text that describes this bot's role, personality, or style.
	// It is appended to the global context (if any) and injected into every Anthropic call
	// made by this bot, after the immutable JSON-output instructions.
	BotContext string `json:"botContext"`
}

// configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration. Any public fields will be deserialized from the Mattermost server configuration
// in OnConfigurationChange.
type configuration struct {
	AnthropicAPIKey string
	// AnthropicModel is the Claude model ID to use (e.g. "claude-sonnet-4-6").
	// Defaults to anthropic.DefaultModel when blank.
	AnthropicModel string
	// AnthropicMaxTokens is stored as a string because Mattermost plugin settings only support
	// text inputs for numeric values. It is parsed to int in plugin.go.
	// Defaults to anthropic.DefaultMaxTokens when blank or unparseable.
	AnthropicMaxTokens string
	// GlobalContext is optional free-text injected into every Anthropic call across all bots,
	// before any per-bot context.  Use it to provide company-wide background information.
	GlobalContext     string
	BotConfigurations string
}

// ParseBotConfigs deserializes the BotConfigurations JSON string into a slice of BotConfig.
func (c *configuration) ParseBotConfigs() ([]BotConfig, error) {
	if c.BotConfigurations == "" || c.BotConfigurations == "[]" {
		return nil, nil
	}
	var configs []BotConfig
	if err := json.Unmarshal([]byte(c.BotConfigurations), &configs); err != nil {
		return nil, errors.Wrap(err, "failed to parse BotConfigurations JSON")
	}
	return configs, nil
}

// Clone shallow copies the configuration.
func (c *configuration) Clone() *configuration {
	clone := *c
	return &clone
}

// getConfiguration retrieves the active configuration under lock, making it safe to use
// concurrently. The active configuration may change underneath the client of this method, but
// the struct returned by this API call is considered immutable.
func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

// setConfiguration replaces the active configuration under lock.
func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}
		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *Plugin) OnConfigurationChange() error {
	configuration := new(configuration)

	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	p.setConfiguration(configuration)

	// Re-register bots whenever configuration changes.
	if p.client != nil {
		if err := p.ensureBots(); err != nil {
			p.API.LogError("Failed to ensure bots after configuration change", "error", err.Error())
		}
	}

	return nil
}
