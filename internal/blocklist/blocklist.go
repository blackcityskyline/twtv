package blocklist

import (
	"os"
	"sort"
	"strings"
)

// Blocklist is a persistent set of blocked channel names (case-insensitive).
type Blocklist struct {
	path string
	set  map[string]struct{}
}

// New loads the blocklist from path; missing file is treated as empty.
func New(path string) *Blocklist {
	bl := &Blocklist{path: path, set: make(map[string]struct{})}

	data, err := os.ReadFile(path)
	if err != nil {
		return bl
	}
	for _, line := range strings.Split(string(data), "\n") {
		if ch := strings.TrimSpace(line); ch != "" {
			bl.set[strings.ToLower(ch)] = struct{}{}
		}
	}
	return bl
}

// Has reports whether channel is blocked (case-insensitive).
func (bl *Blocklist) Has(channel string) bool {
	_, ok := bl.set[strings.ToLower(channel)]
	return ok
}

// Add adds channel to the blocklist and persists the change.
func (bl *Blocklist) Add(channel string) error {
	bl.set[strings.ToLower(channel)] = struct{}{}
	return bl.save()
}

// List returns all blocked channels in sorted order.
func (bl *Blocklist) List() []string {
	out := make([]string, 0, len(bl.set))
	for ch := range bl.set {
		out = append(out, ch)
	}
	sort.Strings(out)
	return out
}

// save writes the current set to disk in sorted order (deterministic diffs).
func (bl *Blocklist) save() error {
	lines := bl.List()
	return os.WriteFile(bl.path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
