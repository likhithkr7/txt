package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.10.0", "0.9.0", true}, // numeric, not string, comparison
		{"1.0.0", "0.99.99", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"v0.2.0", "0.1.0", true},
		{"0.2.0", "dev", false}, // dev builds never prompt
		{"garbage", "0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// fakeReleases serves a GitHub-like releases tree for one version.
func fakeReleases(t *testing.T, version string, binary []byte, checksum string) {
	t.Helper()
	name := AssetName(version)
	if checksum == "" {
		sum := sha256.Sum256(binary)
		checksum = hex.EncodeToString(sum[:])
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v"+version, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/v"+version+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  txt-%s-plan9-mips\n%s  %s\n", strings.Repeat("0", 64), version, checksum, name)
	})
	mux.HandleFunc("/releases/download/v"+version+"/"+name, func(w http.ResponseWriter, r *http.Request) {
		w.Write(binary)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	old := ReleasesURL
	ReleasesURL = srv.URL + "/releases"
	t.Cleanup(func() { ReleasesURL = old })
}

func TestLatest(t *testing.T) {
	fakeReleases(t, "0.3.1", nil, "")
	got, err := Latest(context.Background())
	if err != nil || got != "0.3.1" {
		t.Fatalf("Latest() = %q, %v; want 0.3.1", got, err)
	}
}

func TestLatestNoRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases", http.StatusFound) // what GitHub does with no releases
	}))
	defer srv.Close()
	old := ReleasesURL
	ReleasesURL = srv.URL + "/releases"
	defer func() { ReleasesURL = old }()

	if _, err := Latest(context.Background()); err == nil {
		t.Fatal("expected an error when no release exists")
	}
}

func installed(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "txt")
	if err := os.WriteFile(exe, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestApply(t *testing.T) {
	fakeReleases(t, "0.2.0", []byte("new binary"), "")
	exe := installed(t)

	if err := Apply(context.Background(), "0.2.0", exe); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new binary" {
		t.Errorf("binary = %q, want the new one", got)
	}
	if info, _ := os.Stat(exe); info.Mode().Perm() != 0755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
		t.Errorf("leftover temp files: %d entries", len(entries))
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	fakeReleases(t, "0.2.0", []byte("tampered binary"), strings.Repeat("ab", 32))
	exe := installed(t)

	err := Apply(context.Background(), "0.2.0", exe)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Apply error = %v, want checksum mismatch", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("binary was replaced despite bad checksum: %q", got)
	}
	if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
		t.Errorf("leftover temp files: %d entries", len(entries))
	}
}

func TestApplyNoBuildForPlatform(t *testing.T) {
	fakeReleases(t, "0.2.0", []byte("x"), "")
	// Ask for a version whose checksums list doesn't include this platform
	old := ReleasesURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, strings.Repeat("0", 64)+"  txt-0.2.0-plan9-mips")
	}))
	defer srv.Close()
	ReleasesURL = srv.URL + "/releases"
	defer func() { ReleasesURL = old }()

	exe := installed(t)
	if err := Apply(context.Background(), "0.2.0", exe); err == nil || !strings.Contains(err.Error(), "no build") {
		t.Fatalf("Apply error = %v, want 'no build'", err)
	}
}

// serveSlow answers checksums.txt normally and the binary with handler.
func serveSlow(t *testing.T, binary []byte, handler http.HandlerFunc) string {
	t.Helper()
	sum := sha256.Sum256(binary)
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/download/v0.2.0/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), AssetName("0.2.0"))
	})
	mux.HandleFunc("/releases/download/v0.2.0/"+AssetName("0.2.0"), handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldURL, oldStall := ReleasesURL, StallTimeout
	ReleasesURL, StallTimeout = srv.URL+"/releases", 300*time.Millisecond
	t.Cleanup(func() { ReleasesURL, StallTimeout = oldURL, oldStall })
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestApplyStallBeforeResponse(t *testing.T) {
	host := serveSlow(t, []byte("new"), func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // never answers
	})
	exe := installed(t)
	err := Apply(context.Background(), "0.2.0", exe)
	if !errors.Is(err, ErrStalled) || !strings.Contains(err.Error(), "no data from "+host) {
		t.Fatalf("Apply error = %v, want a stall naming %s", err, host)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("binary changed after a stalled download: %q", got)
	}
}

func TestApplyStallMidDownload(t *testing.T) {
	serveSlow(t, []byte("new binary"), func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("new "))
		w.(http.Flusher).Flush()
		<-r.Context().Done() // stops sending
	})
	exe := installed(t)
	if err := Apply(context.Background(), "0.2.0", exe); !errors.Is(err, ErrStalled) {
		t.Fatalf("Apply error = %v, want ErrStalled", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("binary changed after a stalled download: %q", got)
	}
}

// A slow but steady download is never cut off, however long it takes.
func TestApplySlowButSteady(t *testing.T) {
	binary := []byte("new binary, delivered slowly")
	serveSlow(t, binary, func(w http.ResponseWriter, r *http.Request) {
		for _, b := range binary { // ~1.4s total, far over the 300ms stall limit
			w.Write([]byte{b})
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
	})
	exe := installed(t)
	if err := Apply(context.Background(), "0.2.0", exe); err != nil {
		t.Fatalf("slow download was cut off: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(binary) {
		t.Errorf("binary = %q", got)
	}
}
