package main

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

// initRouter initializes the HTTP router for the plugin.
func (p *Plugin) initRouter() *mux.Router {
	router := mux.NewRouter()

	// Middleware to require that the user is logged in
	router.Use(p.MattermostAuthorizationRequired)

	apiRouter := router.PathPrefix("/api/v1").Subrouter()

	apiRouter.HandleFunc("/hello", p.HelloWorld).Methods(http.MethodGet)
	apiRouter.HandleFunc("/bots/status", p.BotsStatus).Methods(http.MethodGet)
	apiRouter.HandleFunc("/version", p.Version).Methods(http.MethodGet)

	return router
}

// ServeHTTP demonstrates a plugin that handles HTTP requests by greeting the world.
// The root URL is currently <siteUrl>/plugins/com.mattermost.plugin-starter-template/api/v1/. Replace com.mattermost.plugin-starter-template with the plugin ID.
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

func (p *Plugin) MattermostAuthorizationRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-ID")
		if userID == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// BotsStatus re-attempts bot registration and returns a JSON summary of what
// succeeded or failed. Useful for diagnosing configuration problems.
// GET /plugins/com.mattermost.mattermost-trello/api/v1/bots/status
func (p *Plugin) BotsStatus(w http.ResponseWriter, r *http.Request) {
	cfg := p.getConfiguration()

	type botResult struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		UserID      string `json:"userId,omitempty"`
		Error       string `json:"error,omitempty"`
	}

	botConfigs, parseErr := cfg.ParseBotConfigs()

	var results []botResult

	if parseErr != nil {
		results = []botResult{{Error: "BotConfigurations JSON parse error: " + parseErr.Error()}}
	} else if len(botConfigs) == 0 {
		results = []botResult{{Error: "BotConfigurations is empty — paste your JSON array and save the plugin settings first"}}
	} else {
		for _, bc := range botConfigs {
			res := botResult{Username: bc.BotUsername, DisplayName: bc.BotDisplayName}

			botID, ensureErr := p.client.Bot.EnsureBot(&model.Bot{
				Username:    bc.BotUsername,
				DisplayName: bc.BotDisplayName,
				Description: "Mattermost Trello Bot — creates and manages Trello cards from chat threads.",
			})
			if ensureErr != nil {
				res.Error = ensureErr.Error()
			} else {
				res.UserID = botID
				p.botMu.Lock()
				p.botUserIDs[bc.BotUsername] = botID
				p.botMu.Unlock()
			}

			results = append(results, res)
		}
	}

	p.botMu.RLock()
	registered := make(map[string]string)
	for k, v := range p.botUserIDs {
		registered[k] = v
	}
	p.botMu.RUnlock()

	resp := map[string]interface{}{
		"configuredBots":  len(botConfigs),
		"results":         results,
		"registeredInMem": registered,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		p.API.LogError("Failed to encode bots status response", "error", err.Error())
	}
}

// buildTag is bumped on every meaningful release so admins can confirm
// which binary is running via GET /api/v1/version.
const buildTag = "v1.1.0"

// Version returns the plugin build tag as JSON.
func (p *Plugin) Version(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"version": buildTag}); err != nil {
		p.API.LogError("Failed to write version response", "error", err.Error())
	}
}

func (p *Plugin) HelloWorld(w http.ResponseWriter, r *http.Request) {
	if _, err := w.Write([]byte("Hello, world!")); err != nil {
		p.API.LogError("Failed to write response", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
