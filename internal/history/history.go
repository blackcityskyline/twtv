package history

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Entry is a single record in the watch history log.
type Entry struct {
	Time    string
	Channel string
	URL     string
	Game    string
}

// History manages a tab-separated watch-history file with a rolling size limit.
type History struct {
	file  string
	limit int
}

// New returns a History backed by file. limit <= 0 defaults to 500.
func New(file string, limit int) (*History, error) {
	if file == "" {
		d, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		file = d + "/twtv/history.log"
	}
	if limit <= 0 {
		limit = 500
	}
	return &History{file: file, limit: limit}, nil
}

// Read parses and returns all history entries (oldest first).
func (h *History) Read() ([]Entry, error) {
	data, err := os.ReadFile(h.file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	entries := make([]Entry, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 3 {
			continue
		}
		e := Entry{Time: parts[0], Channel: parts[1], URL: parts[2]}
		if len(parts) == 4 {
			e.Game = parts[3]
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Append adds a new entry and trims the file to the configured limit.
func (h *History) Append(channel, url, game string) error {
	line := fmt.Sprintf("%s\t%s\t%s\t%s\n",
		time.Now().Format("2006-01-02 15:04:05"), channel, url, game)

	f, err := os.OpenFile(h.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return err
	}
	f.Close()

	return h.trim()
}

// TopGames returns the n most-watched game names by play count.
func (h *History) TopGames(n int) []string {
	entries, _ := h.Read()

	counts := make(map[string]int, len(entries))
	for _, e := range entries {
		if e.Game != "" {
			counts[e.Game]++
		}
	}

	type kv struct {
		name  string
		count int
	}
	ranked := make([]kv, 0, len(counts))
	for name, count := range counts {
		ranked = append(ranked, kv{name, count})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].count > ranked[j].count })

	result := make([]string, 0, n)
	for i := 0; i < len(ranked) && i < n; i++ {
		result = append(result, ranked[i].name)
	}
	return result
}

// WatchedChannels returns a map of lowercase channel name → watch count.
func (h *History) WatchedChannels() map[string]int {
	entries, _ := h.Read()
	counts := make(map[string]int, len(entries))
	for _, e := range entries {
		counts[strings.ToLower(e.Channel)]++
	}
	return counts
}

// trim truncates the history file to the last h.limit entries.
// It writes atomically via a temp file to avoid data loss on crash.
func (h *History) trim() error {
	entries, err := h.Read()
	if err != nil || len(entries) <= h.limit {
		return err
	}
	entries = entries[len(entries)-h.limit:]

	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", e.Time, e.Channel, e.URL, e.Game)
	}

	tmp := h.file + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, h.file)
}
