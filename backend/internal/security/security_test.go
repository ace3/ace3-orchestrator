package security

import (
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	input := `Authorization: Bearer abc123 token=secret password: "pw" postgres://mp:dbpass@db:5432/app`
	got := RedactSensitive(input)
	for _, leaked := range []string{"abc123", "secret", "dbpass"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted output leaked %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction marker in %q", got)
	}
}

func TestValidateProductionSkillSource(t *testing.T) {
	if err := ValidateProductionSkillSource("https://github.com/ace3/skills", strings.Repeat("a", 40)); err != nil {
		t.Fatalf("expected pinned GitHub URL to pass: %v", err)
	}
	if err := ValidateProductionSkillSource("https://example.com/ace3/skills", strings.Repeat("a", 40)); err == nil {
		t.Fatal("expected non-GitHub URL to fail")
	}
	if err := ValidateProductionSkillSource("https://github.com/ace3/skills", "main"); err == nil {
		t.Fatal("expected non-SHA ref to fail")
	}
}
