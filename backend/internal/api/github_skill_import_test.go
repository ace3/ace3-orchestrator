package api

import "testing"

func TestParseGitHubSkillURLFromTree(t *testing.T) {
	parsed, err := parseGitHubSkillURL("https://github.com/ace3/skills/tree/main/skills/backend-developer")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.CloneURL != "https://github.com/ace3/skills.git" ||
		parsed.Ref != "main" ||
		parsed.PathFilter != "skills/backend-developer/SKILL.md" ||
		parsed.DefaultName != "ace3-skills-backend-developer" {
		t.Fatalf("unexpected parsed URL: %+v", parsed)
	}
}

func TestParseGitHubSkillURLFromBlob(t *testing.T) {
	parsed, err := parseGitHubSkillURL("https://github.com/ace3/skills/blob/v1/skills/qa/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.CloneURL != "https://github.com/ace3/skills.git" ||
		parsed.Ref != "v1" ||
		parsed.PathFilter != "skills/qa/SKILL.md" {
		t.Fatalf("unexpected parsed URL: %+v", parsed)
	}
}

func TestParseGitHubSkillURLRejectsNonSkillBlob(t *testing.T) {
	if _, err := parseGitHubSkillURL("https://github.com/ace3/skills/blob/main/README.md"); err == nil {
		t.Fatal("expected non-skill blob to be rejected")
	}
}
