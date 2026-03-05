package image

import (
	"bufio"
	"io"
	"strings"
)

func ParseOsRelease(r io.Reader) (*OsInfo, error) {
	info := &OsInfo{}
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := unquote(parts[1])

		switch key {
		case "ID":
			info.ID = value
		case "VERSION_ID":
			info.VersionID = value
		case "PRETTY_NAME":
			info.PrettyName = value
		case "HOME_URL":
			info.HomeURL = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return info, nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
