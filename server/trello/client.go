package trello

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const baseURL = "https://api.trello.com/1"

// Client is a minimal Trello REST API client.
type Client struct {
	APIKey   string
	APIToken string
}

// Card represents a newly created Trello card.
type Card struct {
	ID       string `json:"id"`
	ShortURL string `json:"shortUrl"`
	Name     string `json:"name"`
}

// CheckItem represents a single item within a Trello checklist.
type CheckItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"` // "complete" or "incomplete"
}

// Checklist represents a Trello checklist with its items.
type Checklist struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	CheckItems []CheckItem `json:"checkItems"`
}

// CardDetail represents a Trello card with its checklists.
type CardDetail struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	ShortURL   string      `json:"shortUrl"`
	Desc       string      `json:"desc"`
	Checklists []Checklist `json:"checklists"`
}

// CreateCard creates a new Trello card in the specified list.
func (c *Client) CreateCard(listID, name, desc string) (*Card, error) {
	params := url.Values{}
	params.Set("key", c.APIKey)
	params.Set("token", c.APIToken)
	params.Set("idList", listID)
	params.Set("name", name)
	params.Set("desc", desc)

	req, err := http.NewRequest(http.MethodPost, baseURL+"/cards?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("trello: failed to build create card request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trello: failed to create card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("trello: create card returned status %d: %s", resp.StatusCode, string(body))
	}

	var card Card
	if err = json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("trello: failed to decode create card response: %w", err)
	}
	return &card, nil
}

// AddChecklist creates a named checklist on a card and populates it with items.
func (c *Client) AddChecklist(cardID, checklistName string, items []string) error {
	// Create the checklist.
	params := url.Values{}
	params.Set("key", c.APIKey)
	params.Set("token", c.APIToken)
	params.Set("idCard", cardID)
	params.Set("name", checklistName)

	req, err := http.NewRequest(http.MethodPost, baseURL+"/checklists?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("trello: failed to build create checklist request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("trello: failed to create checklist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trello: create checklist returned status %d: %s", resp.StatusCode, string(body))
	}

	var checklist struct {
		ID string `json:"id"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&checklist); err != nil {
		return fmt.Errorf("trello: failed to decode checklist response: %w", err)
	}

	// Add each item.
	for _, item := range items {
		itemParams := url.Values{}
		itemParams.Set("key", c.APIKey)
		itemParams.Set("token", c.APIToken)
		itemParams.Set("name", item)

		itemURL := fmt.Sprintf("%s/checklists/%s/checkItems?%s", baseURL, checklist.ID, itemParams.Encode())
		itemReq, itemErr := http.NewRequest(http.MethodPost, itemURL, nil)
		if itemErr != nil {
			return fmt.Errorf("trello: failed to build add checklist item request for %q: %w", item, itemErr)
		}

		itemResp, itemErr := http.DefaultClient.Do(itemReq)
		if itemErr != nil {
			return fmt.Errorf("trello: failed to add checklist item %q: %w", item, itemErr)
		}
		statusCode := itemResp.StatusCode
		_ = itemResp.Body.Close()

		if statusCode >= 300 {
			return fmt.Errorf("trello: add checklist item %q returned status %d", item, statusCode)
		}
	}

	return nil
}

// AddComment posts a comment on a Trello card.
func (c *Client) AddComment(cardID, text string) error {
	params := url.Values{}
	params.Set("key", c.APIKey)
	params.Set("token", c.APIToken)
	params.Set("text", text)

	req, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("%s/cards/%s/actions/comments?%s", baseURL, cardID, params.Encode()),
		nil,
	)
	if err != nil {
		return fmt.Errorf("trello: failed to build add comment request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("trello: failed to add comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trello: add comment returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetCardWithChecklists fetches a card and all its checklists from Trello.
func (c *Client) GetCardWithChecklists(cardID string) (*CardDetail, error) {
	params := url.Values{}
	params.Set("key", c.APIKey)
	params.Set("token", c.APIToken)
	params.Set("checklists", "all")

	req, err := http.NewRequest(
		http.MethodGet,
		fmt.Sprintf("%s/cards/%s?%s", baseURL, cardID, params.Encode()),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("trello: failed to build get card request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trello: failed to get card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("trello: get card returned status %d: %s", resp.StatusCode, string(body))
	}

	var detail CardDetail
	if err = json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("trello: failed to decode card detail response: %w", err)
	}
	return &detail, nil
}

// UpdateCard updates the name and description of an existing Trello card.
func (c *Client) UpdateCard(cardID, name, desc string) error {
	params := url.Values{}
	params.Set("key", c.APIKey)
	params.Set("token", c.APIToken)
	params.Set("name", name)
	params.Set("desc", desc)

	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/cards/%s?%s", baseURL, cardID, params.Encode()), nil)
	if err != nil {
		return fmt.Errorf("trello: failed to build update card request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("trello: failed to update card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trello: update card returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// UpdateCheckItemState sets the state ("complete" or "incomplete") of a checklist item on a card.
// checklistID is required by the Trello API (idChecklist parameter).
func (c *Client) UpdateCheckItemState(cardID, checklistID, checkItemID, state string) error {
	params := url.Values{}
	params.Set("key", c.APIKey)
	params.Set("token", c.APIToken)
	params.Set("idChecklist", checklistID)
	params.Set("state", state)

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("%s/cards/%s/checkItem/%s?%s", baseURL, cardID, checkItemID, params.Encode()),
		nil,
	)
	if err != nil {
		return fmt.Errorf("trello: failed to build update check item request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("trello: failed to update check item: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trello: update check item returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// AddMemberToCard looks up a Trello member by their username and adds them to the card.
// It assumes the Trello username matches the Mattermost username (as configured).
func (c *Client) AddMemberToCard(cardID, memberUsername string) error {
	// Resolve username → Trello member ID.
	lookupParams := url.Values{}
	lookupParams.Set("key", c.APIKey)
	lookupParams.Set("token", c.APIToken)

	lookupReq, err := http.NewRequest(
		http.MethodGet,
		fmt.Sprintf("%s/members/%s?%s", baseURL, memberUsername, lookupParams.Encode()),
		nil,
	)
	if err != nil {
		return fmt.Errorf("trello: failed to build get member request for %q: %w", memberUsername, err)
	}

	lookupResp, err := http.DefaultClient.Do(lookupReq)
	if err != nil {
		return fmt.Errorf("trello: failed to look up member %q: %w", memberUsername, err)
	}
	defer lookupResp.Body.Close()

	if lookupResp.StatusCode >= 300 {
		body, _ := io.ReadAll(lookupResp.Body)
		return fmt.Errorf("trello: look up member %q returned status %d: %s", memberUsername, lookupResp.StatusCode, string(body))
	}

	var member struct {
		ID string `json:"id"`
	}
	if err = json.NewDecoder(lookupResp.Body).Decode(&member); err != nil {
		return fmt.Errorf("trello: failed to decode member response for %q: %w", memberUsername, err)
	}
	if member.ID == "" {
		return fmt.Errorf("trello: member %q not found", memberUsername)
	}

	// Add the resolved member ID to the card.
	addParams := url.Values{}
	addParams.Set("key", c.APIKey)
	addParams.Set("token", c.APIToken)
	addParams.Set("value", member.ID)

	addReq, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("%s/cards/%s/idMembers?%s", baseURL, cardID, addParams.Encode()),
		nil,
	)
	if err != nil {
		return fmt.Errorf("trello: failed to build add member request: %w", err)
	}

	addResp, err := http.DefaultClient.Do(addReq)
	if err != nil {
		return fmt.Errorf("trello: failed to add member %q to card: %w", memberUsername, err)
	}
	defer addResp.Body.Close()

	if addResp.StatusCode >= 300 {
		body, _ := io.ReadAll(addResp.Body)
		return fmt.Errorf("trello: add member %q to card returned status %d: %s", memberUsername, addResp.StatusCode, string(body))
	}
	return nil
}

// DeleteChecklist removes a checklist from a Trello card.
func (c *Client) DeleteChecklist(checklistID string) error {
	params := url.Values{}
	params.Set("key", c.APIKey)
	params.Set("token", c.APIToken)

	req, err := http.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("%s/checklists/%s?%s", baseURL, checklistID, params.Encode()),
		nil,
	)
	if err != nil {
		return fmt.Errorf("trello: failed to build delete checklist request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("trello: failed to delete checklist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trello: delete checklist returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
