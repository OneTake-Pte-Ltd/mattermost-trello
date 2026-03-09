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

	cmd, rest := parseCommand(messageText)

	switch {
	case cmd != "" && threadCard == nil:
		// Slash commands require a linked Trello card.
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"No Trello card is linked to this thread yet. Mention me at the start of the thread to create one first.")
	case cmd == "update":
		h.handleUpdateCard(post, botUserID, rootPostID, rest, threadCard, cfg)
	case cmd == "done":
		h.handleMarkDone(post, botUserID, rootPostID, rest, threadCard, cfg)
	case cmd == "progress":
		h.handleProgressQuery(post, botUserID, rootPostID, threadCard, cfg)
	case cmd == "freestyle":
		h.handleFreestyle(post, botUserID, rootPostID, threadCard, cfg)
	case cmd == "linear":
		h.handleLinear(post, botUserID, rootPostID, rest, threadCard, cfg)
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

	additionalContext := h.buildAdditionalContext(cfg)
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
		fmt.Sprintf("I've created a Trello card: [%s](%s)\n\nReply in this thread to add more details or ask for `/progress`.", content.Title, card.ShortURL))
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

// handleProgressQuery fetches the Trello card's checklist and posts a formatted progress summary.
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

// handleUpdateCard fetches the existing card + thread, asks Claude to rewrite it, and updates Trello.
func (h *Handler) handleUpdateCard(post *model.Post, botUserID, rootPostID, userMessage string, threadCard *kvstore.ThreadCard, cfg BotConfig) {
	if cfg.TrelloAPIKey == "" || cfg.TrelloAPIToken == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Trello credentials are not configured for this bot. Please ask an admin to set them up.")
		return
	}
	if cfg.AnthropicAPIKey == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"I'm not set up yet — please ask an admin to configure the Anthropic API key.")
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

	cardContext := formatCardContext(detail)
	threadContent := h.getThreadContent(rootPostID)
	additionalContext := h.buildAdditionalContext(cfg)

	ac := &anthropic.Client{APIKey: cfg.AnthropicAPIKey}
	newContent, err := ac.GenerateCardUpdate(cardContext, threadContent, userMessage, cfg.AnthropicModel, cfg.AnthropicMaxTokens, additionalContext)
	if err != nil {
		h.API.LogError("Anthropic GenerateCardUpdate error", "error", err.Error())
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Something went wrong while generating the card update. Please try again or contact an admin.")
		return
	}

	if err = tc.UpdateCard(threadCard.CardID, newContent.Title, newContent.Description); err != nil {
		h.API.LogError("Trello UpdateCard error", "error", err.Error(), "cardID", threadCard.CardID)
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Something went wrong — I couldn't update the Trello card. Please try again or contact an admin.")
		return
	}

	// Replace existing checklists with the new ones from Claude.
	for _, cl := range detail.Checklists {
		if delErr := tc.DeleteChecklist(cl.ID); delErr != nil {
			h.API.LogError("Trello DeleteChecklist error", "error", delErr.Error(), "checklistID", cl.ID)
			// Non-fatal: continue and try to add the new checklist.
		}
	}
	if len(newContent.Checklist) > 0 {
		if err = tc.AddChecklist(threadCard.CardID, "Tasks", newContent.Checklist); err != nil {
			h.API.LogError("Trello AddChecklist error after update", "error", err.Error(), "cardID", threadCard.CardID)
		}
	}

	// Update the stored card URL in case the title changed.
	if err = h.KVStore.SetThreadCard(rootPostID, &kvstore.ThreadCard{
		CardID:      threadCard.CardID,
		CardURL:     threadCard.CardURL,
		BotUsername: cfg.BotUsername,
	}); err != nil {
		h.API.LogError("Failed to update thread card mapping", "error", err.Error())
	}

	h.postAsBot(botUserID, post.ChannelId, rootPostID,
		fmt.Sprintf("I've updated the [Trello card](%s) — title, description, and checklist revised.", threadCard.CardURL))
}

// handleMarkDone uses Claude to identify which checklist items match the user's message, then marks them complete.
func (h *Handler) handleMarkDone(post *model.Post, botUserID, rootPostID, userMessage string, threadCard *kvstore.ThreadCard, cfg BotConfig) {
	if cfg.TrelloAPIKey == "" || cfg.TrelloAPIToken == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Trello credentials are not configured for this bot. Please ask an admin to set them up.")
		return
	}
	if cfg.AnthropicAPIKey == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"I'm not set up yet — please ask an admin to configure the Anthropic API key.")
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

	cardContext := formatCardContext(detail)
	additionalContext := h.buildAdditionalContext(cfg)

	ac := &anthropic.Client{APIKey: cfg.AnthropicAPIKey}
	itemNames, err := ac.IdentifyDoneItems(cardContext, userMessage, cfg.AnthropicModel, cfg.AnthropicMaxTokens, additionalContext)
	if err != nil {
		h.API.LogError("Anthropic IdentifyDoneItems error", "error", err.Error())
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Something went wrong while identifying which items to mark done. Please try again or contact an admin.")
		return
	}

	if len(itemNames) == 0 {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"I couldn't identify any checklist items matching your message. Please be more specific.")
		return
	}

	// Build a name→CheckItem index for fast lookup.
	type itemKey struct{ checklistID, checkItemID string }
	itemIndex := map[string]itemKey{}
	for _, cl := range detail.Checklists {
		for _, item := range cl.CheckItems {
			itemIndex[strings.ToLower(item.Name)] = itemKey{cl.ID, item.ID}
		}
	}

	var marked []string
	for _, name := range itemNames {
		key, ok := itemIndex[strings.ToLower(name)]
		if !ok {
			h.API.LogWarn("CheckItem name returned by Claude not found in card", "name", name, "cardID", threadCard.CardID)
			continue
		}
		if updateErr := tc.UpdateCheckItemState(threadCard.CardID, key.checklistID, key.checkItemID, "complete"); updateErr != nil {
			h.API.LogError("Trello UpdateCheckItemState error", "error", updateErr.Error(), "item", name)
			continue
		}
		marked = append(marked, name)
	}

	if len(marked) == 0 {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"I wasn't able to mark any items as done. Please check the checklist and try again.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Marked %d item(s) as done on the [Trello card](%s):\n", len(marked), threadCard.CardURL))
	for _, name := range marked {
		sb.WriteString(fmt.Sprintf("✅ %s\n", name))
	}
	h.postAsBot(botUserID, post.ChannelId, rootPostID, strings.TrimRight(sb.String(), "\n"))
}

// handleFreestyle generates rap lyrics about the Trello card and posts them.
func (h *Handler) handleFreestyle(post *model.Post, botUserID, rootPostID string, threadCard *kvstore.ThreadCard, cfg BotConfig) {
	if cfg.TrelloAPIKey == "" || cfg.TrelloAPIToken == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Trello credentials are not configured for this bot. Please ask an admin to set them up.")
		return
	}
	if cfg.AnthropicAPIKey == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"I'm not set up yet — please ask an admin to configure the Anthropic API key.")
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

	cardContext := formatCardContext(detail)
	systemPrompt := "You are a creative rap artist. Write fun, energetic rap lyrics about the following Trello project card. Be creative and reference the actual tasks, but keep it workplace-appropriate."

	ac := &anthropic.Client{APIKey: cfg.AnthropicAPIKey}
	lyrics, err := ac.GenerateText(systemPrompt, cardContext, cfg.AnthropicModel, 1024)
	if err != nil {
		h.API.LogError("Anthropic GenerateText error (freestyle)", "error", err.Error())
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Something went wrong while composing the rap. Please try again or contact an admin.")
		return
	}

	h.postAsBot(botUserID, post.ChannelId, rootPostID, lyrics)
}

// handleLinear generates a Linear issue body from the Trello card and Mattermost thread.
func (h *Handler) handleLinear(post *model.Post, botUserID, rootPostID, userMessage string, threadCard *kvstore.ThreadCard, cfg BotConfig) {
	if cfg.TrelloAPIKey == "" || cfg.TrelloAPIToken == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Trello credentials are not configured for this bot. Please ask an admin to set them up.")
		return
	}
	if cfg.AnthropicAPIKey == "" {
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"I'm not set up yet — please ask an admin to configure the Anthropic API key.")
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

	cardContext := formatCardContext(detail)
	threadContent := h.getThreadContent(rootPostID)

	var promptParts []string
	promptParts = append(promptParts, cardContext)
	if threadContent != "" {
		promptParts = append(promptParts, "---\n\nMattermost Thread:\n"+threadContent)
	}
	if userMessage != "" {
		promptParts = append(promptParts, "Additional notes: "+userMessage)
	}
	userPrompt := strings.Join(promptParts, "\n\n")

	ac := &anthropic.Client{APIKey: cfg.AnthropicAPIKey}
	issueBody, err := ac.GenerateText(anthropic.LinearSkillPrompt(), userPrompt, cfg.AnthropicModel, 4096)
	if err != nil {
		h.API.LogError("Anthropic GenerateText error (linear)", "error", err.Error())
		h.postAsBot(botUserID, post.ChannelId, rootPostID,
			"Something went wrong while generating the Linear issue. Please try again or contact an admin.")
		return
	}

	h.postAsBot(botUserID, post.ChannelId, rootPostID, issueBody)
}

// formatCardContext formats a CardDetail into a readable string for Claude prompts.
func formatCardContext(detail *trello.CardDetail) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Card:** %s\n", detail.Name))
	if detail.Desc != "" {
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", detail.Desc))
	}
	if len(detail.Checklists) > 0 {
		for _, cl := range detail.Checklists {
			sb.WriteString(fmt.Sprintf("\n**Checklist: %s**\n", cl.Name))
			for _, item := range cl.CheckItems {
				if item.State == "complete" {
					sb.WriteString(fmt.Sprintf("✅ %s\n", item.Name))
				} else {
					sb.WriteString(fmt.Sprintf("⬜ %s\n", item.Name))
				}
			}
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatProgress formats the Trello card detail into a readable progress summary (fallback).
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

// getThreadContent fetches all posts in the thread and formats them as readable text.
func (h *Handler) getThreadContent(rootPostID string) string {
	postList, appErr := h.API.GetPostThread(rootPostID)
	if appErr != nil {
		h.API.LogWarn("Failed to fetch thread content", "error", appErr.Error(), "rootPostID", rootPostID)
		return ""
	}

	var sb strings.Builder
	for _, postID := range postList.Order {
		p, ok := postList.Posts[postID]
		if !ok || p.Message == "" {
			continue
		}
		user, userErr := h.API.GetUser(p.UserId)
		username := p.UserId
		if userErr == nil {
			username = user.Username
		}
		sb.WriteString(fmt.Sprintf("@%s: %s\n", username, p.Message))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// buildAdditionalContext combines GlobalContext and BotContext.
func (h *Handler) buildAdditionalContext(cfg BotConfig) string {
	var parts []string
	if cfg.GlobalContext != "" {
		parts = append(parts, cfg.GlobalContext)
	}
	if cfg.BotContext != "" {
		parts = append(parts, cfg.BotContext)
	}
	return strings.Join(parts, "\n\n")
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

// parseCommand checks if the message starts with a known slash command.
// Returns the command name (without slash) and the remaining text, or empty strings if no command found.
func parseCommand(msg string) (cmd, rest string) {
	known := []string{"update", "done", "progress", "freestyle", "linear"}
	for _, c := range known {
		prefix := "/" + c
		if strings.EqualFold(msg, prefix) {
			return c, ""
		}
		if strings.HasPrefix(strings.ToLower(msg), prefix+" ") {
			return c, strings.TrimSpace(msg[len(prefix):])
		}
	}
	return "", ""
}

// stripBotMention removes the @botUsername mention from the message text.
func stripBotMention(message, botUsername string) string {
	re := regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(botUsername) + `\s*`)
	return strings.TrimSpace(re.ReplaceAllString(message, ""))
}
