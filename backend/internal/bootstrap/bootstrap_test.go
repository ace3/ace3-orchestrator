package bootstrap

import "testing"

func TestIsPinnedSHA(t *testing.T) {
	if !isPinnedSHA("0123456789abcdef0123456789abcdef01234567") {
		t.Fatal("expected 40-char hex string to be treated as pinned SHA")
	}
	if isPinnedSHA("main") {
		t.Fatal("branch name should not be treated as pinned SHA")
	}
}
