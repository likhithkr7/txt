package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"unicode/utf8"
)

var (
	ErrTooLarge = errors.New("file exceeds size limit")
	ErrNotUTF8  = errors.New("file is not valid UTF-8")
	ErrConflict = errors.New("file modified externally")
)

const MaxFileSize = 4 * 1024 * 1024 // 4 MB

// SessionFile stores open tabs and unsaved text between runs. It lives at the
// workspace root and is hidden (dot-prefixed), so it never shows in the tree.
const SessionFile = ".txt-session.json"

// MaxSessionSize caps the session file; it may hold several unsaved files.
const MaxSessionSize = 32 * 1024 * 1024 // 32 MB

type Workspace struct {
	root *os.Root
}

type Node struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	IsDir    bool    `json:"isDir"`
	Children []*Node `json:"children,omitempty"`
}

type FileData struct {
	Content    string `json:"content"`
	Version    string `json:"version"`
	LineEnding string `json:"lineEnding"`
}

type SaveRequest struct {
	Content     string `json:"content"`
	BaseVersion string `json:"baseVersion"`
	LineEnding  string `json:"lineEnding"`
}

type SaveResponse struct {
	Version string `json:"version"`
	Path    string `json:"path,omitempty"` // set by CreateFile: the final name, after NormalizeFileName
}

// Extensions txt opens and lists. Anything else is treated as part of the name.
var textExtensions = map[string]bool{".txt": true, ".md": true}

// IsTextFile reports whether name has an extension txt can open (.txt or .md).
func IsTextFile(name string) bool {
	return textExtensions[strings.ToLower(path.Ext(name))]
}

// NormalizeFileName adds ".txt" unless the name already ends in a supported
// extension, so "notes" and "meeting.2026" become "notes.txt" and
// "meeting.2026.txt", while "readme.md" stays as it is.
func NormalizeFileName(rel string) string {
	if IsTextFile(rel) {
		return rel
	}
	return rel + ".txt"
}

func (w *Workspace) Tree() (*Node, error) {
	// w.root.FS() gives us an io/fs.FS restricted to the sandbox
	return buildTree(w.root.FS(), ".")
}

func (w *Workspace) CreateFile(rel string) (*SaveResponse, error) {
	rel = NormalizeFileName(rel)
	if err := ValidatePath(rel, true); err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// O_EXCL ensures we NEVER overwrite an existing file
	f, err := w.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return nil, err
	}
	f.Close()

	// Return the initial version for the frontend state
	info, err := w.root.Stat(rel)
	if err != nil {
		return nil, err
	}
	version := fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())

	return &SaveResponse{Version: version, Path: rel}, nil
}

func (w *Workspace) CreateDir(rel string) error {
	// Validate as a directory (isFile = false)
	if err := ValidatePath(rel, false); err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// We use standard Mkdir. You could write a loop to implement MkdirAll
	// using w.root.Mkdir if you want to support deep creation (e.g. a/b/c)
	err := w.root.Mkdir(rel, 0755)
	if err != nil {
		return err
	}

	return nil
}

func (w *Workspace) ReadFile(rel string) (*FileData, error) {
	// 1. Enforce product rules
	if err := ValidatePath(rel, true); err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// 2. Open safely within the sandbox
	f, err := w.root.OpenFile(rel, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxFileSize {
		return nil, ErrTooLarge
	}

	// 3. Read with a hard limit to prevent TOCTOU size changes
	limitReader := io.LimitReader(f, MaxFileSize+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, ErrTooLarge
	}

	// 4. Validate UTF-8
	if !utf8.Valid(data) {
		return nil, ErrNotUTF8
	}

	// 5. Detect and normalize line endings
	contentStr := string(data)
	lineEnding := "lf"
	if strings.Contains(contentStr, "\r\n") {
		lineEnding = "crlf"
		contentStr = strings.ReplaceAll(contentStr, "\r\n", "\n")
	}

	// 6. Compute version
	version := fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())

	return &FileData{
		Content:    contentStr,
		Version:    version,
		LineEnding: lineEnding,
	}, nil
}

func (w *Workspace) SaveFile(rel string, req SaveRequest) (*SaveResponse, error) {
	if err := ValidatePath(rel, true); err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// 1. Optimistic Concurrency: Check current version
	currentData, err := w.ReadFile(rel)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err // Error reading existing file
	}

	var originalMode os.FileMode = 0644
	if err == nil {
		if currentData.Version != req.BaseVersion {
			return nil, ErrConflict
		}
		// Grab original permissions to preserve them
		if info, err := w.root.Stat(rel); err == nil {
			originalMode = info.Mode()
		}
	}

	// 2. Format content: restore line endings and ensure trailing newline
	content := req.Content
	if req.LineEnding == "crlf" {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
	if !strings.HasSuffix(content, "\n") && !strings.HasSuffix(content, "\r\n") {
		if req.LineEnding == "crlf" {
			content += "\r\n"
		} else {
			content += "\n"
		}
	}

	// 3. Write atomically (temp file + rename), keeping the original permissions
	if err := w.writeAtomic(rel, []byte(content), originalMode); err != nil {
		return nil, err
	}

	// 4. Return the newly computed version
	info, err := w.root.Stat(rel)
	if err != nil {
		return nil, err
	}
	newVersion := fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())

	return &SaveResponse{Version: newVersion}, nil
}

// writeAtomic replaces rel with data so readers never see a half-written
// file: write a hidden temp file in the same directory, fsync, then rename.
func (w *Workspace) writeAtomic(rel string, data []byte, mode os.FileMode) error {
	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return err
	}
	tempPath := path.Join(path.Dir(rel), fmt.Sprintf(".%s-save-%s.tmp", path.Base(rel), hex.EncodeToString(randBytes)))

	tempFile, err := w.root.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	// Clean up the temp file if anything fails before the rename
	defer w.root.Remove(tempPath)

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return err
	}
	// fsync flushes the OS buffer to the physical disk
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	// os.Root can't fsync the directory itself, but data sync + atomic
	// rename is sufficient here
	return w.root.Rename(tempPath, rel)
}

// ReadSession returns the saved editor session (open tabs, unsaved text), or
// nil if there is none yet. The contents are opaque to the server.
func (w *Workspace) ReadSession() ([]byte, error) {
	f, err := w.root.Open(SessionFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxSessionSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSessionSize {
		return nil, ErrTooLarge
	}
	return data, nil
}

// WriteSession replaces the saved editor session. The file is owner-only
// because it can hold unsaved text.
func (w *Workspace) WriteSession(data []byte) error {
	if len(data) > MaxSessionSize {
		return ErrTooLarge
	}
	return w.writeAtomic(SessionFile, data, 0600)
}

func buildTree(fileSystem fs.FS, dirPath string) (*Node, error) {
	entries, err := fs.ReadDir(fileSystem, dirPath)
	if err != nil {
		return nil, err
	}

	var children []*Node

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files and folders
		if strings.HasPrefix(name, ".") {
			continue
		}

		// Clean paths to use forward slashes (e.g., "folder/file.txt")
		fullPath := path.Join(dirPath, name)
		if dirPath == "." {
			fullPath = name
		}

		if entry.IsDir() {
			// Recurse into the directory
			childNode, err := buildTree(fileSystem, fullPath)
			if err != nil {
				return nil, err
			}
			// Add the folder if it contains a valid file, or if it is empty
			// (e.g. freshly created from the UI). Folders holding only
			// other kinds of files stay hidden.
			if len(childNode.Children) > 0 || isEmptyDir(fileSystem, fullPath) {
				children = append(children, childNode)
			}
		} else {
			// Include only files txt can open
			if IsTextFile(name) {
				children = append(children, &Node{
					Name:  name,
					Path:  fullPath,
					IsDir: false,
				})
			}
		}
	}

	return &Node{
		Name:     path.Base(dirPath),
		Path:     dirPath,
		IsDir:    true,
		Children: children,
	}, nil
}

// isEmptyDir reports whether a directory has no visible (non-hidden) entries.
func isEmptyDir(fileSystem fs.FS, dirPath string) bool {
	entries, err := fs.ReadDir(fileSystem, dirPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			return false
		}
	}
	return true
}

// Open creates a secure boundary around the given directory.
func Open(dir string) (*Workspace, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open workspace root: %w", err)
	}
	return &Workspace{root: r}, nil
}

// ValidatePath enforces the application's rules on paths.
// It assumes the path is meant to be relative to the workspace root.
func ValidatePath(rel string, isFile bool) error {
	if rel == "" {
		return errors.New("path cannot be empty")
	}
	if strings.Contains(rel, "\\") {
		return errors.New("paths must use forward slashes (/)")
	}

	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" {
			return errors.New("path cannot contain empty segments")
		}
		if part == "." || part == ".." {
			// os.Root prevents escapes, but we reject them outright for product clarity
			return errors.New("path cannot contain . or .. segments")
		}
		if strings.HasPrefix(part, ".") {
			return errors.New("hidden files and folders are not allowed")
		}
	}

	if isFile && !IsTextFile(rel) {
		return errors.New("files must end in .txt or .md")
	}

	return nil
}
