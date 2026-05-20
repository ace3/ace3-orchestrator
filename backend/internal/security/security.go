package security

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	assignmentSecretPattern = regexp.MustCompile(`(?i)\b(token|api[_-]?key|secret|password|passwd|pwd|authorization)\b\s*[:=]\s*['"]?[^'"\s,;]+`)
	bearerPattern           = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
	dsnPasswordPattern      = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:\s]+:)[^@\s]+(@)`)
)

func RedactSensitive(value string) string {
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = dsnPasswordPattern.ReplaceAllString(value, "${1}[REDACTED]${2}")
	return assignmentSecretPattern.ReplaceAllStringFunc(value, func(match string) string {
		sep := strings.IndexAny(match, ":=")
		if sep < 0 {
			return "[REDACTED]"
		}
		return strings.TrimSpace(match[:sep+1]) + " [REDACTED]"
	})
}

func IsPinnedSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func ValidateProductionSkillSource(upstreamURL, pinnedSHA string) error {
	parsed, err := url.Parse(strings.TrimSpace(upstreamURL))
	if err != nil {
		return err
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if parsed.Scheme != "https" || host != "github.com" {
		return fmt.Errorf("production skill sources must use https://github.com URLs")
	}
	if !IsPinnedSHA(strings.TrimSpace(pinnedSHA)) {
		return fmt.Errorf("production skill sources must be pinned to a 40-character commit SHA")
	}
	return nil
}
