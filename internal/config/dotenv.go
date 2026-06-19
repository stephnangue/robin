package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// parseDotenv parses a flat KEY=value file: blank lines and #-comments are
// skipped, an optional leading "export " is stripped, and a single layer of
// surrounding single/double quotes is removed. No interpolation, no nesting.
func parseDotenv(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for line := 1; sc.Scan(); line++ {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		raw = strings.TrimPrefix(raw, "export ")
		eq := strings.IndexByte(raw, '=')
		if eq < 0 {
			return nil, fmt.Errorf("line %d: missing '='", line)
		}
		key := strings.TrimSpace(raw[:eq])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", line)
		}
		out[key] = unquote(strings.TrimSpace(raw[eq+1:]))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		if c := s[0]; (c == '"' || c == '\'') && s[len(s)-1] == c {
			return s[1 : len(s)-1]
		}
	}
	return s
}
