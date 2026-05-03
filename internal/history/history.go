package history

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Entry struct {
	Timestamp string
	Channel   string
	URL       string
}

func (e Entry) String() string {
	return fmt.Sprintf("%s\t%s\t%s", e.Timestamp, e.Channel, e.URL)
}

type History struct {
	path  string
	limit int
}

func New(dir string, limit int) *History {
	return &History{
		path:  filepath.Join(dir, "history.log"),
		limit: limit,
	}
}

func (h *History) Append(channel, url string) error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	ts := time.Now().Format("2006-01-02 15:04:05")
	_, err = fmt.Fprintf(f, "%s\t%s\t%s\n", ts, channel, url)
	if err != nil {
		return err
	}

	return h.trim()
}

// Read returns all entries, most recent last.
func (h *History) Read() ([]Entry, error) {
	f, err := os.Open(h.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		entries = append(entries, Entry{
			Timestamp: parts[0],
			Channel:   parts[1],
			URL:       parts[2],
		})
	}
	return entries, scanner.Err()
}

// Last returns the last n entries.
func (h *History) Last(n int) ([]Entry, error) {
	all, err := h.Read()
	if err != nil || len(all) <= n {
		return all, err
	}
	return all[len(all)-n:], nil
}

// trim keeps only the last h.limit lines.
func (h *History) trim() error {
	if h.limit <= 0 {
		return nil
	}
	entries, err := h.Read()
	if err != nil || len(entries) <= h.limit {
		return err
	}
	entries = entries[len(entries)-h.limit:]

	f, err := os.Create(h.path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, e := range entries {
		fmt.Fprintln(w, e.String())
	}
	return w.Flush()
}
