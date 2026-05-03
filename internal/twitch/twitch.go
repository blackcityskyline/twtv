package twitch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const apiBase = "https://api.twitch.tv/helix"

type Client struct {
	clientID    string
	accessToken string
	http        *http.Client
}

func New(clientID, accessToken string) *Client {
	return &Client{
		clientID:    clientID,
		accessToken: accessToken,
		http:        &http.Client{Timeout: 10 * time.Second},
	}
}

type Stream struct {
	UserLogin   string `json:"user_login"`
	UserName    string `json:"user_name"`
	GameName    string `json:"game_name"`
	Title       string `json:"title"`
	ViewerCount int    `json:"viewer_count"`
	StartedAt   string `json:"started_at"`
}

type Follow struct {
	BroadcasterLogin string `json:"broadcaster_login"`
	BroadcasterName  string `json:"broadcaster_name"`
}

func (c *Client) get(url string, out any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(c.accessToken, "oauth:"))

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("twitch API %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// UserID resolves the current user's ID from the token.
func (c *Client) UserID() (string, error) {
	var res struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.get(apiBase+"/users", &res); err != nil {
		return "", err
	}
	if len(res.Data) == 0 {
		return "", fmt.Errorf("no user found — check your access token")
	}
	return res.Data[0].ID, nil
}

// Follows returns all channels the user follows (paginates automatically).
func (c *Client) Follows(userID string) ([]Follow, error) {
	var follows []Follow
	cursor := ""
	for {
		url := fmt.Sprintf("%s/channels/followed?user_id=%s&first=100", apiBase, userID)
		if cursor != "" {
			url += "&after=" + cursor
		}
		var res struct {
			Data []Follow `json:"data"`
			Pagination struct {
				Cursor string `json:"cursor"`
			} `json:"pagination"`
		}
		if err := c.get(url, &res); err != nil {
			return nil, err
		}
		follows = append(follows, res.Data...)
		if res.Pagination.Cursor == "" || len(res.Data) == 0 {
			break
		}
		cursor = res.Pagination.Cursor
	}
	return follows, nil
}

// LiveStreams returns currently live streams for the given logins (max 100 per call).
func (c *Client) LiveStreams(logins []string) ([]Stream, error) {
	var streams []Stream
	// API allows up to 100 logins per request
	for i := 0; i < len(logins); i += 100 {
		chunk := logins[i:min(i+100, len(logins))]
		url := apiBase + "/streams?first=100&" + joinParams("user_login", chunk)
		var res struct {
			Data []Stream `json:"data"`
		}
		if err := c.get(url, &res); err != nil {
			return nil, err
		}
		streams = append(streams, res.Data...)
	}
	return streams, nil
}

func joinParams(key string, vals []string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = key + "=" + v
	}
	return strings.Join(parts, "&")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
