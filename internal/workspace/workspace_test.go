package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSandboxEscapes(t *testing.T) {
	// Create a temporary directory for the workspace
	dir := t.TempDir()

	// Create a secret file *outside* the workspace to act as the target
	secretPath := filepath.Join(dir, "../secret.txt")
	os.WriteFile(secretPath, []byte("you should not see this"), 0644)

	ws, err := Open(dir)
	if err != nil {
		t.Fatalf("Failed to open workspace: %v", err)
	}

	// Attempt 1: Direct traversal string
	t.Run("Direct Traversal", func(t *testing.T) {
		_, err := ws.root.Open("../secret.txt")
		if err == nil {
			t.Fatal("Expected error when trying to escape via ../, got none")
		}
	})

	// Attempt 2: Absolute path
	t.Run("Absolute Path", func(t *testing.T) {
		_, err := ws.root.Open(secretPath)
		if err == nil {
			t.Fatal("Expected error when opening absolute path, got none")
		}
	})

	// Note: os.Root prevents following symlinks out of the root automatically.
}

func TestTreeShowsEmptyDirs(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "empty"), 0755)
	os.MkdirAll(filepath.Join(dir, "nested/inner"), 0755)
	os.Mkdir(filepath.Join(dir, "binaries"), 0755)
	os.WriteFile(filepath.Join(dir, "binaries/app.bin"), []byte{0}, 0644)

	ws, err := Open(dir)
	if err != nil {
		t.Fatalf("Failed to open workspace: %v", err)
	}
	tree, err := ws.Tree()
	if err != nil {
		t.Fatalf("Tree failed: %v", err)
	}

	got := map[string]bool{}
	for _, c := range tree.Children {
		got[c.Name] = true
	}
	if !got["empty"] {
		t.Error("expected empty dir to be listed")
	}
	if !got["nested"] {
		t.Error("expected dir containing an empty dir to be listed")
	}
	if got["binaries"] {
		t.Error("expected dir with only non-.txt files to be hidden")
	}
}

// "Save as" from an untitled tab saves to a new path with an empty base version.
func TestSaveFileToNewPath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "taken.txt"), []byte("existing\n"), 0644)
	ws, err := Open(dir)
	if err != nil {
		t.Fatalf("Failed to open workspace: %v", err)
	}

	if _, err := ws.SaveFile("fresh.txt", SaveRequest{Content: "hello", LineEnding: "lf"}); err != nil {
		t.Fatalf("expected new file to save, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "fresh.txt")); string(got) != "hello\n" {
		t.Errorf("unexpected content %q", got)
	}

	if _, err := ws.SaveFile("taken.txt", SaveRequest{Content: "clobber"}); !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict for existing file, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "taken.txt")); string(got) != "existing\n" {
		t.Errorf("existing file was modified: %q", got)
	}

	if _, err := ws.SaveFile("missing/x.txt", SaveRequest{Content: "x"}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected ErrNotExist for missing folder, got %v", err)
	}
}

func TestSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0644)
	ws, err := Open(dir)
	if err != nil {
		t.Fatalf("Failed to open workspace: %v", err)
	}

	if data, err := ws.ReadSession(); err != nil || data != nil {
		t.Fatalf("expected no session yet, got %q, %v", data, err)
	}

	want := `{"tabs":[{"path":"a.txt"}]}`
	if err := ws.WriteSession([]byte(want)); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	if data, err := ws.ReadSession(); err != nil || string(data) != want {
		t.Errorf("ReadSession = %q, %v; want %q", data, err, want)
	}

	info, err := os.Stat(filepath.Join(dir, SessionFile))
	if err != nil {
		t.Fatalf("session file missing: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("session file mode = %v, want 0600 (it can hold unsaved text)", perm)
	}

	// Hidden from the sidebar, and no temp files left behind
	tree, _ := ws.Tree()
	if len(tree.Children) != 1 || tree.Children[0].Name != "a.txt" {
		t.Errorf("tree should only list a.txt, got %+v", tree.Children)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("expected a.txt and the session file only, got %d entries", len(entries))
	}

	if err := ws.WriteSession(make([]byte, MaxSessionSize+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("expected ErrTooLarge for oversized session, got %v", err)
	}
}

// os.Root must stop symlinks from reaching files outside the workspace.
func TestSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret\n"), 0644)
	dir := t.TempDir()
	os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt"))
	ws, _ := Open(dir)
	if _, err := ws.ReadFile("link.txt"); err == nil {
		t.Fatal("read through symlink escaped the workspace")
	}
	if _, err := ws.SaveFile("link.txt", SaveRequest{Content: "x"}); err == nil {
		if b, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(b) != "secret\n" {
			t.Fatal("write through symlink modified a file outside the workspace")
		}
	}
}

func TestNormalizeFileName(t *testing.T) {
	cases := map[string]string{
		"notes":           "notes.txt",
		"notes.txt":       "notes.txt",
		"readme.md":       "readme.md",
		"README.MD":       "README.MD",
		"meeting.2026":    "meeting.2026.txt", // unknown extension: part of the name
		"draft.md.bak":    "draft.md.bak.txt",
		"journal/today":   "journal/today.txt",
		"v1.2/release.md": "v1.2/release.md",
	}
	for in, want := range cases {
		if got := NormalizeFileName(in); got != want {
			t.Errorf("NormalizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hi\n"), 0644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("n\n"), 0644)
	os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}\n"), 0644)
	ws, err := Open(dir)
	if err != nil {
		t.Fatalf("Failed to open workspace: %v", err)
	}

	tree, _ := ws.Tree()
	var names []string
	for _, c := range tree.Children {
		names = append(names, c.Name)
	}
	if len(names) != 2 || names[0] != "notes.txt" || names[1] != "readme.md" {
		t.Errorf("tree = %v, want [notes.txt readme.md]", names)
	}

	if data, err := ws.ReadFile("readme.md"); err != nil || data.Content != "# Hi\n" {
		t.Errorf("ReadFile(readme.md) = %+v, %v", data, err)
	}
	if _, err := ws.ReadFile("data.json"); err == nil {
		t.Error("expected .json to be refused")
	}

	for in, want := range map[string]string{"plan.md": "plan.md", "plain": "plain.txt", "q3.2026": "q3.2026.txt"} {
		resp, err := ws.CreateFile(in)
		if err != nil || resp.Path != want {
			t.Errorf("CreateFile(%q) = %+v, %v; want path %q", in, resp, err, want)
		}
	}
}

// One unreadable folder must not hide the rest of the workspace.
func TestTreeSkipsUnreadableDirs(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read everything")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("ok\n"), 0644)
	locked := filepath.Join(dir, "locked")
	os.Mkdir(locked, 0755)
	os.WriteFile(filepath.Join(locked, "secret.txt"), []byte("s\n"), 0644)
	os.Chmod(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0755) })

	ws, err := Open(dir)
	if err != nil {
		t.Fatalf("Failed to open workspace: %v", err)
	}
	tree, err := ws.Tree()
	if err != nil {
		t.Fatalf("Tree failed because of one unreadable folder: %v", err)
	}
	if len(tree.Children) != 1 || tree.Children[0].Name != "ok.txt" {
		t.Errorf("tree = %+v, want just ok.txt", tree.Children)
	}
}
