package handler

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/mattermost/mattermost-plugin-starter-template/server/anthropic"
	"github.com/mattermost/mattermost-plugin-starter-template/server/store/kvstore"
	"github.com/mattermost/mattermost-plugin-starter-template/server/trello"
)

// BotConfig mirrors the configuration.BotConfig to avoid a circular import.
type BotConfig struct {
	BotUsername    string
	BotDisplayName string
	TrelloAPIKey   string
	TrelloAPIToken string
	TrelloBoardID  string
	TrelloListID   string
	// BotContext is optional per-bot context injected into Anthropic calls (appended after GlobalContext).
	BotContext string
	// Anthropic settings resolved from plugin configuration.
	AnthropicAPIKey    string
	AnthropicModel     string
	AnthropicMaxTokens int
	GlobalContext      string
}

// Handler processes bot-mention messages and coordinates Anthropic + Trello actions.
type Handler struct {
	API     plugin.API
	KVStore kvstore.KVStore
}

var progressKeywords = []string{"progress", "status", "update"}

// Handle is the main entry point. It is called from a goroutine for each post that mentions
// a configured bot.
func (h *Handler) Handle(post *model.Post, botUsername, botUserID string, cfg BotConfig) {
	rootPostID := post.RootId
	if rootPostID == "" {
		rootPostID = post.Id
	}

	messageText := stripBotMention(post.Message, botUsername)

	threadCard, err := h.KVStore.GetThreadCard(rootPostID)
	if err != nil {
		h.API.LogError("Failed to get thread card from KV store", "error", err.Error(), "rootPostID", rootPostID)
	}

	switch {
	case isProgressQuery(messageText) && threadCard != nil:
		h.handleProgressQuery(post, botUserID, rootPostID, threadCard, cfg)
	case threadCard != nil:
		h.handleAddDetails(post, botUserID, rootPostID, messageText, threadCard, cfg)
	default:
		h.handleCreateCard(post, botUserID, rootPostID, messageText, cfg)
	}
}

// handleCreateCard uses Claude to generate card content then creates the Trello card.
func (h *Handler) handleCreateCard(post *model.Post, botUserID, rootPostID, messageText string, cfg BotConfig) {
	if cfg.TrelloAPIKey == "" || cfg.TrelloAPIToken == "" || cfg.TrelloListID == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Trello credentials are not configured for this bot. Please ask an admin to set them up.")
		return
	}

	if cfg.AnthropicAPIKey == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"I'm not set up yet — please ask an admin to configure the Anthropic API key.")
		return
	}

	// Combine global and per-bot context; each part is only included if non-empty.
	var contextParts []string
	if cfg.GlobalContext != "" {
		contextParts = append(contextParts, cfg.GlobalContext)
	}
	if cfg.BotContext != "" {
		contextParts = append(contextParts, cfg.BotContext)
	}
	additionalContext := strings.Join(contextParts, "\n\n")

	threadLink := h.buildThreadLink(post, rootPostID)

	ac := &anthropic.Client{APIKey: cfg.AnthropicAPIKey}
	content, err := ac.GenerateCardContent(messageText, threadLink, cfg.AnthropicModel, cfg.AnthropicMaxTokens, additionalContext)
	if err != nil {
		h.API.LogError("Anthropic API error", "error", err.Error())
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Something went wrong while generating the card content. Please try again or contact an admin.")
		return
	}

	tc := &trello.Client{APIKey: cfg.TrelloAPIKey, APIToken: cfg.TrelloAPIToken}
	card, err := tc.CreateCard(cfg.TrelloListID, content.Title, content.Description)
	if err != nil {
		h.API.LogError("Trello CreateCard error", "error", err.Error())
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Something went wrong — I couldn't create the Trello card. Please try again or contact an admin.")
		return
	}

	if len(content.Checklist) > 0 {
		if err = tc.AddChecklist(card.ID, "Tasks", content.Checklist); err != nil {
			h.API.LogError("Trello AddChecklist error", "error", err.Error(), "cardID", card.ID)
			// Non-fatal: the card exists, just without the checklist.
		}
	}

	if err = h.KVStore.SetThreadCard(rootPostID, &kvstore.ThreadCard{
		CardID:      card.ID,
		CardURL:     card.ShortURL,
		BotUsername: cfg.BotUsername,
	}); err != nil {
		h.API.LogError("Failed to save thread card mapping", "error", err.Error())
	}

	h.postAsBot(botUserID, post.ChannelId, rootPostID,
		fmt.Sprintf("I've created a Trello card: [%s](%s)\n\nReply in this thread to add more details or ask for `progress`.", content.Title, card.ShortURL))
}

// handleAddDetails adds the user's follow-up message as a comment on the linked Trello card.
func (h *Handler) handleAddDetails(post *model.Post, botUserID, rootPostID, messageText string, threadCard *kvstore.ThreadCard, cfg BotConfig) {
	if cfg.TrelloAPIKey == "" || cfg.TrelloAPIToken == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Trello credentials are not configured for this bot. Please ask an admin to set them up.")
		return
	}

	tc := &trello.Client{APIKey: cfg.TrelloAPIKey, APIToken: cfg.TrelloAPIToken}

	author := h.getPostAuthorUsername(post.UserId)
	commentText := fmt.Sprintf("**Update from @%s (via Mattermost):**\n\n%s", author, messageText)

	if err := tc.AddComment(threadCard.CardID, commentText); err != nil {
		h.API.LogError("Trello AddComment error", "error", err.Error(), "cardID", threadCard.CardID)
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Something went wrong — I couldn't add that to the Trello card. Please try again or contact an admin.")
		return
	}

	h.postAsBot(botUserID, post.ChannelId, rootPostID,
		fmt.Sprintf("Got it — I've added that to the [Trello card](%s).", threadCard.CardURL))
}

// handleProgressQuery fetches the Trello card's checklist and posts a summary.
func (h *Handler) handleProgressQuery(post *model.Post, botUserID, rootPostID string, threadCard *kvstore.ThreadCard, cfg BotConfig) {
	if cfg.TrelloAPIKey == "" || cfg.TrelloAPIToken == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Trello credentials are not configured for this bot. Please ask an admin to set them up.")
		return
	}

	tc := &trello.Client{APIKey: cfg.TrelloAPIKey, APIToken: cfg.TrelloAPIToken}
	detail, err := tc.GetCardWithChecklists(threadCard.CardID)
	if err != nil {
		h.API.LogError("Trello GetCardWithChecklists error", "error", err.Error(), "cardID", threadCard.CardID)
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Something went wrong — I couldn't fetch the Trello card. Please try again or contact an admin.")
		return
	}

	h.postAsBot(botUserID, post.ChannelId, rootPostID, formatProgress(detail))
}

// formatProgress formats the Trello card detail into a readable progress summary.
func formatProgress(detail *trello.CardDetail) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**[%s](%s)**\n\n", detail.Name, detail.ShortURL))

	if len(detail.Checklists) == 0 {
		sb.WriteString("No checklists found on this card.")
		return sb.String()
	}

	for _, cl := range detail.Checklists {
		total := len(cl.CheckItems)
		done := 0
		for _, item := range cl.CheckItems {
			if item.State == "complete" {
				done++
			}
		}
		sb.WriteString(fmt.Sprintf("**%s — %d/%d completed**\n", cl.Name, done, total))
		for _, item := range cl.CheckItems {
			if item.State == "complete" {
				sb.WriteString(fmt.Sprintf("✅ %s\n", item.Name))
			} else {
				sb.WriteString(fmt.Sprintf("⬜ %s\n", item.Name))
			}
		}
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// postAsBot creates a post in a channel/thread on behalf of a bot user.
func (h *Handler) postAsBot(botUserID, channelID, rootPostID, message string) {
	post := &model.Post{
		UserId:    botUserID,
		ChannelId: channelID,
		RootId:    rootPostID,
		Message:   message,
	}
	if _, err := h.API.CreatePost(post); err != nil {
		h.API.LogError("Failed to post bot message", "error", err.Error())
	}
}

// buildThreadLink constructs a permalink to the Mattermost thread.
func (h *Handler) buildThreadLink(post *model.Post, rootPostID string) string {
	cfg := h.API.GetConfig()
	siteURL := ""
	if cfg.ServiceSettings.SiteURL != nil {
		siteURL = *cfg.ServiceSettings.SiteURL
	}

	channel, appErr := h.API.GetChannel(post.ChannelId)
	if appErr != nil {
		return siteURL
	}

	team, appErr := h.API.GetTeam(channel.TeamId)
	if appErr != nil {
		return siteURL
	}

	return fmt.Sprintf("%s/%s/pl/%s", siteURL, team.Name, rootPostID)
}

// getPostAuthorUsername retrieves the username of the post author.
func (h *Handler) getPostAuthorUsername(userID string) string {
	user, appErr := h.API.GetUser(userID)
	if appErr != nil {
		return userID
	}
	return user.Username
}

// stripBotMention removes the @botUsername mention from the message text.
func stripBotMention(message, botUsername string) string {
	re := regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(botUsername) + `\s*`)
	return strings.TrimSpace(re.ReplaceAllString(message, ""))
}

// isProgressQuery returns true if the message is asking for a progress or status update.
func isProgressQuery(message string) bool {
	lower := strings.ToLower(message)
	for _, kw := range progressKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
