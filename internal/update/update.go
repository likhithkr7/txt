// Package update checks GitHub Releases for newer versions of txt and
// replaces the running binary with one. It uses the same release assets as
// install.sh: txt-<version>-<os>-<arch>[.exe] plus checksums.txt.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases come from.
const Repo = "likhithkr7/txt"

// ReleasesURL is the base for release pages and downloads (a variable so
// tests can point it at a local server).
var ReleasesURL = "https://github.com/" + Repo + "/releases"

// maxBinarySize guards against a bogus download filling the disk.
const maxBinarySize = 200 << 20 // 200 MB

// Latest returns the newest release version, without the "v" prefix.
//
// It reads the redirect from /releases/latest to /releases/tag/<tag>, which,
// unlike the GitHub API, has no rate limit.
func Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, ReleasesURL+"/latest", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if i == -1 {
		return "", errors.New("no release published yet")
	}
	return strings.TrimPrefix(loc[i+len("/tag/"):], "v"), nil
}

// Newer reports whether version a is newer than b. Versions are
// "major.minor.patch"; a non-numeric version (like "dev") is never newer.
func Newer(a, b string) bool {
	pa, okA := parse(a)
	pb, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// AssetName is the release file for this OS and CPU.
func AssetName(version string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("txt-%s-%s-%s%s", version, runtime.GOOS, runtime.GOARCH, ext)
}

// Apply downloads the given version, verifies its checksum, and replaces the
// binary at exe with it. The new file is written next to exe and renamed
// over it, so exe is never left half-written.
func Apply(ctx context.Context, version, exe string) error {
	name := AssetName(version)
	base := fmt.Sprintf("%s/download/v%s/", ReleasesURL, version)

	want, err := checksumFor(ctx, base+"checksums.txt", name)
	if err != nil {
		return err
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".txt-update-*")
	if err != nil {
		return fmt.Errorf("can't write to %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	body, err := get(ctx, base+name)
	if err != nil {
		tmp.Close()
		return err
	}
	defer body.Close()

	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(body, maxBinarySize+1))
	if err == nil && n > maxBinarySize {
		err = errors.New("download is unexpectedly large")
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("downloading %s: %w", name, err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s); not installing", name, want, got)
	}
	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		// A running .exe can't be overwritten, but it can be renamed aside
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), exe)
}

// checksumFor finds name's SHA-256 in a checksums.txt ("<hash>  <name>" lines).
func checksumFor(ctx context.Context, url, name string) (string, error) {
	body, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer body.Close()
	sc := bufio.NewScanner(io.LimitReader(body, 1<<20))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no build of this version for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// StallTimeout is how long a download may go without receiving any data
// before it's abandoned. There's no limit on total time, so a slow but
// working connection is never cut off. (A variable so tests can shorten it.)
var StallTimeout = 30 * time.Second

// ErrStalled is returned when a download makes no progress for StallTimeout.
var ErrStalled = errors.New("download stalled")

// get starts a GET and returns its body. A watchdog cancels the request if no
// data arrives for StallTimeout, whether waiting for the response or reading it.
func get(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(StallTimeout, func() { cancel(ErrStalled) })
	fail := func(err error) (io.ReadCloser, error) {
		timer.Stop()
		cancel(nil)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fail(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(context.Cause(ctx), ErrStalled) {
			// url.Error names the request that stalled, after any redirects
			host := req.URL.Host
			var uerr *url.Error
			if errors.As(err, &uerr) {
				if u, perr := url.Parse(uerr.URL); perr == nil {
					host = u.Host
				}
			}
			return fail(stalled(host))
		}
		return fail(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fail(fmt.Errorf("GET %s: %s", rawURL, resp.Status))
	}
	timer.Reset(StallTimeout)
	return &watchedBody{body: resp.Body, ctx: ctx, cancel: cancel, timer: timer, host: resp.Request.URL.Host}, nil
}

func stalled(host string) error {
	return fmt.Errorf("%w: no data from %s for %d seconds", ErrStalled, host, int(StallTimeout.Seconds()))
}

// watchedBody resets the stall watchdog whenever data arrives.
type watchedBody struct {
	body   io.ReadCloser
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  *time.Timer
	host   string
}

func (b *watchedBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.timer.Reset(StallTimeout)
	}
	if err != nil && err != io.EOF && errors.Is(context.Cause(b.ctx), ErrStalled) {
		err = stalled(b.host)
	}
	return n, err
}

func (b *watchedBody) Close() error {
	b.timer.Stop()
	b.cancel(nil)
	return b.body.Close()
}
