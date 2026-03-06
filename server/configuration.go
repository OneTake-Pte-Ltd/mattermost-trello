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
}

// configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration. Any public fields will be deserialized from the Mattermost server configuration
// in OnConfigurationChange.
type configuration struct {
	AnthropicAPIKey   string
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
