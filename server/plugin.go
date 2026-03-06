package main

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
	"github.com/pkg/errors"

	"github.com/mattermost/mattermost-plugin-starter-template/server/command"
	"github.com/mattermost/mattermost-plugin-starter-template/server/handler"
	"github.com/mattermost/mattermost-plugin-starter-template/server/store/kvstore"
)

// Plugin implements the interface expected by the Mattermost server to communicate between the
// server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	// kvstore is the client used to read/write KV records for this plugin.
	kvstore kvstore.KVStore

	// client is the Mattermost server API client.
	client *pluginapi.Client

	// commandClient is the client used to register and execute slash commands.
	commandClient command.Command

	// router is the HTTP router for handling API requests.
	router *mux.Router

	backgroundJob *cluster.Job

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration.
	configuration *configuration

	// botUserIDs maps each configured bot username to its Mattermost user ID.
	botUserIDs map[string]string
	botMu      sync.RWMutex

	// wg tracks in-flight message-handling goroutines so OnDeactivate can wait for them.
	wg sync.WaitGroup
}

// OnActivate is invoked when the plugin is activated. If an error is returned, the plugin will be deactivated.
func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)
	p.kvstore = kvstore.NewKVStore(p.client)
	p.commandClient = command.NewCommandHandler(p.client)
	p.router = p.initRouter()
	p.botUserIDs = make(map[string]string)

	if err := p.ensureBots(); err != nil {
		// Log but do not fail activation — bots can be registered after config is set.
		p.API.LogWarn("Could not register bots on activation (check BotConfigurations in plugin settings)", "error", err.Error())
	}

	job, err := cluster.Schedule(
		p.API,
		"BackgroundJob",
		cluster.MakeWaitForRoundedInterval(1*time.Hour),
		p.runJob,
	)
	if err != nil {
		return errors.Wrap(err, "failed to schedule background job")
	}

	p.backgroundJob = job

	return nil
}

// OnDeactivate is invoked when the plugin is deactivated.
func (p *Plugin) OnDeactivate() error {
	if p.backgroundJob != nil {
		if err := p.backgroundJob.Close(); err != nil {
			p.API.LogError("Failed to close background job", "err", err)
		}
	}
	// Wait for all in-flight message handlers to finish.
	p.wg.Wait()
	return nil
}

// ExecuteCommand executes registered slash commands.
func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	response, err := p.commandClient.Handle(args)
	if err != nil {
		return nil, model.NewAppError("ExecuteCommand", "plugin.command.execute_command.app_error", nil, err.Error(), http.StatusInternalServerError)
	}
	return response, nil
}

// MessageHasBeenPosted is called for every post. It checks for bot mentions and dispatches
// handling to a goroutine.
func (p *Plugin) MessageHasBeenPosted(_ *plugin.Context, post *model.Post) {
	// Skip system messages.
	if post.Type != "" {
		return
	}

	// Skip messages posted by our own bots (prevents infinite loops).
	p.botMu.RLock()
	for _, botUserID := range p.botUserIDs {
		if post.UserId == botUserID {
			p.botMu.RUnlock()
			return
		}
	}
	p.botMu.RUnlock()

	// Check whether any configured bot is mentioned.
	cfg := p.getConfiguration()
	botConfigs, err := cfg.ParseBotConfigs()
	if err != nil {
		p.API.LogError("Failed to parse bot configurations", "error", err.Error())
		return
	}

	lowerMessage := strings.ToLower(post.Message)
	for _, bc := range botConfigs {
		if !strings.Contains(lowerMessage, "@"+strings.ToLower(bc.BotUsername)) {
			continue
		}

		p.botMu.RLock()
		botUserID, ok := p.botUserIDs[bc.BotUsername]
		p.botMu.RUnlock()

		if !ok {
			p.API.LogWarn("Bot user ID not found; bot may not be registered yet", "botUsername", bc.BotUsername)
			continue
		}

		h := &handler.Handler{
			API:     p.API,
			KVStore: p.kvstore,
		}

		handlerCfg := handler.BotConfig{
			BotUsername:    bc.BotUsername,
			BotDisplayName: bc.BotDisplayName,
			TrelloAPIKey:   bc.TrelloAPIKey,
			TrelloAPIToken: bc.TrelloAPIToken,
			TrelloBoardID:  bc.TrelloBoardID,
			TrelloListID:   bc.TrelloListID,
		}

		// Capture loop variables for the goroutine.
		postCopy := post
		botUserIDCopy := botUserID
		bcCopy := bc.BotUsername
		hCfg := handlerCfg

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			h.Handle(postCopy, bcCopy, botUserIDCopy, hCfg)
		}()

		// Only one bot can be mentioned per message; stop after the first match.
		break
	}
}

// ensureBots registers (or re-registers) all configured bot accounts with Mattermost.
func (p *Plugin) ensureBots() error {
	cfg := p.getConfiguration()
	botConfigs, err := cfg.ParseBotConfigs()
	if err != nil {
		return errors.Wrap(err, "failed to parse bot configurations")
	}
	if len(botConfigs) == 0 {
		return nil
	}

	p.botMu.Lock()
	defer p.botMu.Unlock()

	for _, bc := range botConfigs {
		botID, appErr := p.API.EnsureBotUser(&model.Bot{
			Username:    bc.BotUsername,
			DisplayName: bc.BotDisplayName,
			Description: "Mattermost Trello Bot — creates and manages Trello cards from chat threads.",
		})
		if appErr != nil {
			p.API.LogError("Failed to ensure bot user",
				"botUsername", bc.BotUsername,
				"error", appErr.Error())
			continue
		}
		p.botUserIDs[bc.BotUsername] = botID
		p.API.LogInfo("Bot registered", "botUsername", bc.BotUsername, "botUserID", botID)
	}

	return nil
}

// See https://developers.mattermost.com/extend/plugins/server/reference/
