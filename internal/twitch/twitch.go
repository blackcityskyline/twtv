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
	UserLogin    string `json:"user_login"`
	UserName     string `json:"user_name"`
	GameID       string `json:"game_id"`
	GameName     string `json:"game_name"`
	Title        string `json:"title"`
	ViewerCount  int    `json:"viewer_count"`
	StartedAt    string `json:"started_at"`
	ThumbnailURL string `json:"thumbnail_url"`
}

type Game struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Follow struct {
	BroadcasterLogin string `json:"broadcaster_login"`
	BroadcasterName  string `json:"broadcaster_name"`
}

// ChannelInfo holds extended information about a broadcaster.
type ChannelInfo struct {
	ID              string `json:"id"`
	Login           string `json:"login"`
	DisplayName     string `json:"display_name"`
	Description     string `json:"description"`
	ProfileImageURL string `json:"profile_image_url"`
	GameName        string `json:"game_name"`
	Title           string `json:"title"`
	FollowerCount   int
	IsFollowing     bool
	IsLive          bool
	ViewerCount     int
	StartedAt       string
}

// Video represents a past broadcast, highlight or upload.
type Video struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	CreatedAt    string `json:"created_at"`
	PublishedAt  string `json:"published_at"`
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	ViewCount    int    `json:"view_count"`
	Duration     string `json:"duration"`
	Type         string `json:"type"` // "archive" | "highlight" | "upload"
}

// Clip represents a Twitch clip.
type Clip struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	CreatedAt    string  `json:"created_at"`
	URL          string  `json:"url"`
	ThumbnailURL string  `json:"thumbnail_url"`
	ViewCount    int     `json:"view_count"`
	Duration     float64 `json:"duration"`
}

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

// ChannelInfoByLogin fetches full channel info for a given login.
// Makes up to 5 API calls: users, channels, followers, follow-check, stream.
func (c *Client) ChannelInfoByLogin(login, authUserID string) (*ChannelInfo, error) {
	var usersRes struct {
		Data []struct {
			ID              string `json:"id"`
			Login           string `json:"login"`
			DisplayName     string `json:"display_name"`
			Description     string `json:"description"`
			ProfileImageURL string `json:"profile_image_url"`
		} `json:"data"`
	}
	if err := c.get(apiBase+"/users?login="+url.QueryEscape(login), &usersRes); err != nil {
		return nil, fmt.Errorf("users lookup: %w", err)
	}
	if len(usersRes.Data) == 0 {
		return nil, fmt.Errorf("user not found: %s", login)
	}
	u := usersRes.Data[0]
	info := &ChannelInfo{
		ID:              u.ID,
		Login:           u.Login,
		DisplayName:     u.DisplayName,
		Description:     u.Description,
		ProfileImageURL: u.ProfileImageURL,
	}

	var chanRes struct {
		Data []struct {
			GameName string `json:"game_name"`
			Title    string `json:"title"`
		} `json:"data"`
	}
	if err := c.get(apiBase+"/channels?broadcaster_id="+u.ID, &chanRes); err == nil && len(chanRes.Data) > 0 {
		info.GameName = chanRes.Data[0].GameName
		info.Title = chanRes.Data[0].Title
	}

	var follRes struct {
		Total int `json:"total"`
	}
	if err := c.get(fmt.Sprintf("%s/channels/followers?broadcaster_id=%s&first=1", apiBase, u.ID), &follRes); err == nil {
		info.FollowerCount = follRes.Total
	}

	if authUserID != "" {
		var followRes struct {
			Data []struct{} `json:"data"`
		}
		chk := fmt.Sprintf("%s/channels/followed?user_id=%s&broadcaster_id=%s", apiBase, authUserID, u.ID)
		if err := c.get(chk, &followRes); err == nil {
			info.IsFollowing = len(followRes.Data) > 0
		}
	}

	var streamRes struct {
		Data []struct {
			ViewerCount int    `json:"viewer_count"`
			StartedAt   string `json:"started_at"`
		} `json:"data"`
	}
	if err := c.get(apiBase+"/streams?user_login="+url.QueryEscape(login), &streamRes); err == nil && len(streamRes.Data) > 0 {
		info.IsLive = true
		info.ViewerCount = streamRes.Data[0].ViewerCount
		info.StartedAt = streamRes.Data[0].StartedAt
	}

	return info, nil
}

// Videos returns VODs/highlights for a broadcaster (newest first).
// vtype: "archive" | "highlight" | "upload" | "" (all).
func (c *Client) Videos(broadcasterID string, limit int, vtype string) ([]Video, error) {
	u := fmt.Sprintf("%s/videos?user_id=%s&first=%d&sort=time", apiBase, broadcasterID, limit)
	if vtype != "" {
		u += "&type=" + url.QueryEscape(vtype)
	}
	var res struct {
		Data []Video `json:"data"`
	}
	if err := c.get(u, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// Clips returns clips for a broadcaster (most viewed first).
func (c *Client) Clips(broadcasterID string, limit int) ([]Clip, error) {
	u := fmt.Sprintf("%s/clips?broadcaster_id=%s&first=%d", apiBase, broadcasterID, limit)
	var res struct {
		Data []Clip `json:"data"`
	}
	if err := c.get(u, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

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
