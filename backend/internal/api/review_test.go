package api

import "testing"

func TestParseUnifiedDiffHandlesAddDeleteAndMultipleHunks(t *testing.T) {
	raw := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
 package main
+func added() {}
 func kept() {}
@@ -10,2 +11,1 @@
-func removed() {}
 func tail() {}
diff --git a/new.txt b/new.txt
new file mode 100644
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
diff --git a/old.txt b/old.txt
deleted file mode 100644
--- a/old.txt
+++ /dev/null
@@ -1,1 +0,0 @@
-bye
`
	files := parseUnifiedDiff(raw)
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3", len(files))
	}
	if files[0].Path != "a.go" || files[0].Additions != 1 || files[0].Deletions != 1 || len(files[0].Hunks) != 2 {
		t.Fatalf("unexpected first file parse: %+v", files[0])
	}
	if files[1].Path != "new.txt" || files[1].Additions != 2 || files[1].Deletions != 0 {
		t.Fatalf("unexpected added file parse: %+v", files[1])
	}
	if files[2].Path != "old.txt" || files[2].Additions != 0 || files[2].Deletions != 1 {
		t.Fatalf("unexpected deleted file parse: %+v", files[2])
	}
	if files[0].Hunks[0].Lines[1].Kind != "add" || files[0].Hunks[0].Lines[1].NewLine == nil || *files[0].Hunks[0].Lines[1].NewLine != 2 {
		t.Fatalf("unexpected added line: %+v", files[0].Hunks[0].Lines[1])
	}
}
