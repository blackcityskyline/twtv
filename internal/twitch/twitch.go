package twitch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBase = "https://api.twitch.tv/helix"

// Client is an authenticated Twitch Helix API client.
type Client struct {
	clientID    string
	accessToken string
	http        *http.Client
}

// New returns a Client configured with the given credentials.
func New(clientID, accessToken string) *Client {
	return &Client{
		clientID:    clientID,
		accessToken: accessToken,
		http:        &http.Client{Timeout: 10 * time.Second},
	}
}

// Stream represents a live Twitch stream.
type Stream struct {
	UserLogin    string `json:"user_login"`
	UserName     string `json:"user_name"`
	GameID       string `json:"game_id"`
	GameName     string `json:"game_name"`
	Title        string `json:"title"`
	ViewerCount  int    `json:"viewer_count"`
	StartedAt    string `json:"started_at"`
	ThumbnailURL string `json:"thumbnail_url"` // contains {width} and {height} placeholders
}

// Game represents a Twitch category / game.
type Game struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Follow represents a single followed channel.
type Follow struct {
	BroadcasterLogin string `json:"broadcaster_login"`
	BroadcasterName  string `json:"broadcaster_name"`
}

// get performs an authenticated GET and decodes the JSON response.
func (c *Client) get(rawURL string, out any) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("twitch API %s: %s", rawURL, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// UserID resolves the authenticated user's ID from the token.
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

// Follows returns all channels the authenticated user follows (auto-paginated).
func (c *Client) Follows(userID string) ([]Follow, error) {
	var follows []Follow
	cursor := ""
	for {
		u := fmt.Sprintf("%s/channels/followed?user_id=%s&first=100", apiBase, userID)
		if cursor != "" {
			u += "&after=" + cursor
		}
		var res struct {
			Data       []Follow `json:"data"`
			Pagination struct {
				Cursor string `json:"cursor"`
			} `json:"pagination"`
		}
		if err := c.get(u, &res); err != nil {
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

// FollowedStreams returns live streams of all channels the user follows (auto-paginated).
func (c *Client) FollowedStreams(userID string) ([]Stream, error) {
	var streams []Stream
	cursor := ""
	for {
		u := fmt.Sprintf("%s/streams/followed?user_id=%s&first=100", apiBase, userID)
		if cursor != "" {
			u += "&after=" + cursor
		}
		var res struct {
			Data       []Stream `json:"data"`
			Pagination struct {
				Cursor string `json:"cursor"`
			} `json:"pagination"`
		}
		if err := c.get(u, &res); err != nil {
			return nil, err
		}
		streams = append(streams, res.Data...)
		if res.Pagination.Cursor == "" || len(res.Data) == 0 {
			break
		}
		cursor = res.Pagination.Cursor
	}
	return streams, nil
}

// LiveStreams returns currently live streams for the given logins (batched in chunks of 100).
func (c *Client) LiveStreams(logins []string) ([]Stream, error) {
	var streams []Stream
	for i := 0; i < len(logins); i += 100 {
		chunk := logins[i:min(i+100, len(logins))]
		u := apiBase + "/streams?first=100&" + buildQuery("user_login", chunk)
		var res struct {
			Data []Stream `json:"data"`
		}
		if err := c.get(u, &res); err != nil {
			return nil, err
		}
		streams = append(streams, res.Data...)
	}
	return streams, nil
}

// TopStreams returns the most-viewed live streams globally.
func (c *Client) TopStreams(limit int) ([]Stream, error) {
	u := fmt.Sprintf("%s/streams?first=%d", apiBase, limit)
	var res struct {
		Data []Stream `json:"data"`
	}
	if err := c.get(u, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// TopGames returns the most popular categories / games.
func (c *Client) TopGames(limit int) ([]Game, error) {
	u := fmt.Sprintf("%s/games/top?first=%d", apiBase, limit)
	var res struct {
		Data []Game `json:"data"`
	}
	if err := c.get(u, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// StreamsByGame returns live streams for the given game ID.
func (c *Client) StreamsByGame(gameID string, limit int) ([]Stream, error) {
	u := fmt.Sprintf("%s/streams?game_id=%s&first=%d", apiBase, gameID, limit)
	var res struct {
		Data []Stream `json:"data"`
	}
	if err := c.get(u, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GamesByName looks up games by their display name.
func (c *Client) GamesByName(names []string) ([]Game, error) {
	u := apiBase + "/games?" + buildQuery("name", names)
	var res struct {
		Data []Game `json:"data"`
	}
	if err := c.get(u, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// buildQuery builds a repeated query-string parameter, URL-encoding each value.
func buildQuery(key string, vals []string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = key + "=" + url.QueryEscape(v)
	}
	return strings.Join(parts, "&")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
