package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Find returns the terminal emulator binary by walking up the process tree.
// Falls back to $TERMINAL env var if set.
func Find() (string, error) {
	if t := os.Getenv("TERMINAL"); t != "" {
		return t, nil
	}

	skip := map[string]bool{
		"python3": true, "python": true,
		"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true,
	}

	pid := os.Getppid()
	for pid > 1 {
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err != nil {
			break
		}
		name := strings.TrimSpace(string(comm))
		if !skip[name] {
			if path, err := exec.LookPath(name); err == nil {
				return path, nil
			}
		}
		// read PPid from /proc/<pid>/status
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			break
		}
		pid = 0
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "PPid:") {
				pid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
				break
			}
		}
	}
	return "", fmt.Errorf("could not detect terminal — set $TERMINAL in your environment or config")
}
