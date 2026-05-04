// Package muted manages the persistent list of muted (hidden) channels.
// Storage is a plain text file — one lowercase channel name per line.
// Plain text is intentional: simple to read, edit, and diff by hand.
package muted

import (
	"os"
	"sort"
	"strings"
)

// List is the in-memory mute list backed by a text file.
type List struct {
	path string
	set  map[string]struct{}
}

// Load reads the mute list from path. A missing file is treated as empty.
func Load(path string) *List {
	ml := &List{path: path, set: make(map[string]struct{})}

	data, err := os.ReadFile(path)
	if err != nil {
		return ml
	}
	for _, line := range strings.Split(string(data), "\n") {
		if ch := strings.TrimSpace(line); ch != "" {
			ml.set[strings.ToLower(ch)] = struct{}{}
		}
	}
	return ml
}

// Has reports whether channel is muted (case-insensitive).
func (ml *List) Has(channel string) bool {
	_, ok := ml.set[strings.ToLower(channel)]
	return ok
}

// Mute adds channel and persists the change.
func (ml *List) Mute(channel string) error {
	ml.set[strings.ToLower(channel)] = struct{}{}
	return ml.save()
}

// Unmute removes channel and persists the change.
func (ml *List) Unmute(channel string) error {
	delete(ml.set, strings.ToLower(channel))
	return ml.save()
}

// Channels returns all muted channel names in sorted order.
func (ml *List) Channels() []string {
	out := make([]string, 0, len(ml.set))
	for ch := range ml.set {
		out = append(out, ch)
	}
	sort.Strings(out)
	return out
}

// save writes the set to disk in sorted order (deterministic, git-friendly).
func (ml *List) save() error {
	lines := ml.Channels()
	return os.WriteFile(ml.path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
