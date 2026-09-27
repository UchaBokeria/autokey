package dashboard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// tailFile returns the last n lines of a file (empty slice when missing).
func tailFile(path string, n int) ([]string, error) {
	if path == "" {
		return []string{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("open log: %w", err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return lines, sc.Err()
}
