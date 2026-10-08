package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"txt/internal/update"
	"txt/internal/workspace"
	"txt/web"
)

// A custom wrapper to capture the HTTP status code
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// loggingMiddleware logs one line per request (enabled with -v)
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Default to 200 OK
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		// Pass the wrapped writer to the actual handler
		next.ServeHTTP(sw, r)

		// Log after the request is finished
		log.Printf("%s %s %d %v", r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}

func securityMiddleware(expectedPort int, expectedToken string, next http.Handler) http.Handler {
	validHost1 := fmt.Sprintf("127.0.0.1:%d", expectedPort)
	validHost2 := fmt.Sprintf("localhost:%d", expectedPort)
	expectedOrigin := fmt.Sprintf("http://127.0.0.1:%d", expectedPort)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only txt's own scripts may run. Markdown previews are sanitized,
		// and this is the second line of defence: a malicious .md file must
		// never get script access to the file API. Remote images are allowed
		// so previews can show them.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; "+
			"style-src 'self'; img-src 'self' data: https:; object-src 'none'; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// 1. Host check (DNS rebinding protection)
		if r.Host != validHost1 && r.Host != validHost2 {
			http.Error(w, "Invalid Host header", http.StatusForbidden)
			return
		}

		// Apply deeper checks only to API routes
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// 2. Cookie check (Session validation)
			cookie, err := r.Cookie("txt_session")
			if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(expectedToken)) != 1 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// 3. Origin check for mutating requests (CSRF protection)
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
				origin := r.Header.Get("Origin")
				if origin != expectedOrigin {
					http.Error(w, "Invalid Origin", http.StatusForbidden)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// openBrowser asks the OS to open url in the default browser.
func openBrowser(url string) error {
	var err error
	switch runtime.GOOS {
	case "darwin": // macOS
		err = exec.Command("open", url).Start()
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	return err
}

// runUpdate replaces this binary with the latest release (txt -update).
func runUpdate() {
	if version == "dev" {
		fatalf("this is a development build; to switch to a release, use the installer:\n" +
			"  curl -fsSL https://raw.githubusercontent.com/" + update.Repo + "/main/install.sh | sh")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fmt.Println("Checking for updates...")
	latest, err := update.Latest(ctx)
	if err != nil {
		fatalf("checking for updates: %v", err)
	}
	if !update.Newer(latest, version) {
		fmt.Printf("txt v%s is the latest version.\n", version)
		return
	}

	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fatalf("finding the txt binary: %v", err)
	}

	fmt.Printf("Downloading txt v%s...\n", latest)
	if err := update.Apply(ctx, latest, exe); err != nil {
		if errors.Is(err, os.ErrPermission) {
			fatalf("%v\nNo permission to replace %s; try: sudo txt -update", err, exe)
		}
		fatalf("updating: %v", err)
	}
	fmt.Printf("Updated txt v%s -> v%s\n", version, latest)
}

// fatalf prints an error in the usual "prog: message" form and exits.
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "txt: "+format+"\n", args...)
	os.Exit(1)
}

// version is set at build time by the release workflow:
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/txt
var version = "dev"

func main() {
	port := flag.Int("port", 7777, "port to listen on (falls back to a free port if busy)")
	noOpen := flag.Bool("no-open", false, "don't open a browser; just print the link")
	verbose := flag.Bool("v", false, "log every HTTP request")
	showVersion := flag.Bool("version", false, "print the version and exit")
	doUpdate := flag.Bool("update", false, "update txt to the latest release and exit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: txt [flags] [dir | file.txt | file.md]")
		fmt.Fprintln(os.Stderr, "\nEdit the .txt and .md files in a folder (default: the current one) in your browser.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("txt %s\n", version)
		return
	}
	if *doUpdate {
		runUpdate()
		return
	}

	args := flag.Args()
	if len(args) > 1 {
		flag.Usage()
		os.Exit(2)
	}

	target := "."
	if len(args) == 1 {
		target = args[0]
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		fatalf("generating security token: %v", err)
	}
	expectedToken := hex.EncodeToString(tokenBytes)

	absPath, err := filepath.Abs(target)
	if err != nil {
		fatalf("resolving %s: %v", target, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		fatalf("%s: no such file or directory", absPath)
	}

	var workspaceRoot string
	var initialFile string

	if info.IsDir() {
		workspaceRoot = absPath
	} else {
		if !workspace.IsTextFile(absPath) {
			fatalf("%s: only .txt and .md files can be opened", absPath)
		}
		workspaceRoot = filepath.Dir(absPath)
		// Store the file relative to the workspace root
		initialFile = filepath.Base(absPath)
	}

	ws, err := workspace.Open(workspaceRoot)
	if err != nil {
		fatalf("%v", err)
	}

	// Listen on loopback only; fall back to any free port if the requested one is taken
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fatalf("starting server: %v", err)
		}
		fmt.Fprintf(os.Stderr, "txt: port %d is in use, using %d instead\n", *port, listener.Addr().(*net.TCPAddr).Port)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	finalURL := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	// Set up the HTTP router
	mux := http.NewServeMux()
	// The core embedded static files
	staticHandler := http.FileServerFS(web.Assets)

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// If they provide a token in the URL, verify it
		queryToken := r.URL.Query().Get("token")
		if queryToken != "" {
			// Use constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(queryToken), []byte(expectedToken)) == 1 {
				// Set a strict session cookie
				http.SetCookie(w, &http.Cookie{
					Name:     "txt_session",
					Value:    expectedToken,
					Path:     "/",
					HttpOnly: true,                    // JavaScript cannot read this cookie
					SameSite: http.SameSiteStrictMode, // Prevents sending on cross-site requests
				})
				// Redirect to strip the token from the URL, keeping any
				// file to open (set when txt was started with a file)
				dest := "/"
				if open := r.URL.Query().Get("open"); open != "" {
					dest += "?" + url.Values{"open": {open}}.Encode()
				}
				http.Redirect(w, r, dest, http.StatusFound)
				return
			} else {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}
		}

		// Otherwise, serve the static frontend
		staticHandler.ServeHTTP(w, r)
	})

	// One folder level for the sidebar (?path=dir, default the root)
	mux.HandleFunc("GET /api/tree", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("path")
		if dir == "" {
			dir = "."
		}
		list, err := ws.ListDir(dir)
		if err != nil {
			switch {
			case errors.Is(err, os.ErrNotExist):
				http.Error(w, "Folder not found", http.StatusNotFound)
			case errors.Is(err, os.ErrPermission):
				http.Error(w, "No permission to read this folder", http.StatusForbidden)
			case strings.Contains(err.Error(), "invalid path"):
				http.Error(w, err.Error(), http.StatusBadRequest)
			default:
				http.Error(w, "Failed to read folder", http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	})

	mux.HandleFunc("GET /api/file", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "missing path parameter", http.StatusBadRequest)
			return
		}

		data, err := ws.ReadFile(path)
		if err != nil {
			// Map errors to HTTP status codes
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "File not found", http.StatusNotFound)
			} else if errors.Is(err, workspace.ErrTooLarge) {
				http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
			} else if errors.Is(err, workspace.ErrNotUTF8) {
				http.Error(w, "Unsupported media type", http.StatusUnsupportedMediaType)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	})

	mux.HandleFunc("POST /api/file", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "missing path parameter", http.StatusBadRequest)
			return
		}

		resp, err := ws.CreateFile(path)
		if err != nil {
			if os.IsExist(err) {
				http.Error(w, "File already exists", http.StatusConflict)
			} else if strings.Contains(err.Error(), "invalid path") {
				http.Error(w, err.Error(), http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("POST /api/dir", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "missing path parameter", http.StatusBadRequest)
			return
		}

		err := ws.CreateDir(path)
		if err != nil {
			if os.IsExist(err) {
				http.Error(w, "Directory already exists", http.StatusConflict)
			} else if strings.Contains(err.Error(), "invalid path") {
				http.Error(w, err.Error(), http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	mux.HandleFunc("PUT /api/file", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "missing path parameter", http.StatusBadRequest)
			return
		}

		// Enforce size limit on the incoming request body
		r.Body = http.MaxBytesReader(w, r.Body, workspace.MaxFileSize+1024) // +1024 for JSON overhead

		var req workspace.SaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		resp, err := ws.SaveFile(path, req)
		if err != nil {
			if errors.Is(err, workspace.ErrConflict) {
				http.Error(w, "Conflict: file changed externally", http.StatusConflict)
			} else if errors.Is(err, os.ErrNotExist) {
				// Saving to a new path whose folder doesn't exist
				http.Error(w, "Folder not found", http.StatusNotFound)
			} else if strings.Contains(err.Error(), "invalid path") {
				http.Error(w, err.Error(), http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Editor session (open tabs, unsaved text). The frontend owns the format;
	// the server only checks it's JSON and stores it in the workspace.
	// Version info for the UI's "update available" notice. latestRelease is
	// filled in by the background check below, if it finds something newer.
	var latestRelease atomic.Pointer[string]
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]string{"version": version}
		if latest := latestRelease.Load(); latest != nil {
			info["latest"] = *latest
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})

	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		data, err := ws.ReadSession()
		if err != nil {
			http.Error(w, "Failed to read session", http.StatusInternalServerError)
			return
		}
		if data == nil {
			data = []byte("null")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	mux.HandleFunc("PUT /api/session", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, workspace.MaxSessionSize))
		if err != nil {
			http.Error(w, "Session too large", http.StatusRequestEntityTooLarge)
			return
		}
		if !json.Valid(data) {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := ws.WriteSession(data); err != nil {
			http.Error(w, "Failed to save session", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	var handler http.Handler = securityMiddleware(actualPort, expectedToken, mux)
	if *verbose {
		handler = loggingMiddleware(handler)
	}

	// The login link carries this run's secret token. When the browser opens
	// it for us there's no need to show the token; it's printed only when
	// you have to open the link yourself (-no-open, or no browser).
	query := url.Values{"token": {expectedToken}}
	if initialFile != "" {
		query.Set("open", initialFile)
	}
	loginURL := finalURL + "/?" + query.Encode()

	fmt.Printf("txt is serving %s\n\n", workspaceRoot)
	opened := false
	if !*noOpen {
		if err := openBrowser(loginURL); err != nil {
			fmt.Fprintf(os.Stderr, "txt: couldn't open a browser (%v)\n\n", err)
		} else {
			opened = true
		}
	}
	if opened {
		fmt.Printf("  Opened %s in your browser.\n\n", finalURL)
	} else {
		fmt.Printf("  Open this link to start (it includes a one-time login token):\n  %s\n\n", loginURL)
	}
	fmt.Println("Press Ctrl+C to stop.")

	// Look for a newer release in the background; never delays startup.
	// Skipped for local builds and when TXT_NO_UPDATE_CHECK is set.
	if version != "dev" && os.Getenv("TXT_NO_UPDATE_CHECK") == "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			latest, err := update.Latest(ctx)
			if err != nil || !update.Newer(latest, version) {
				return
			}
			latestRelease.Store(&latest)
			fmt.Printf("\nUpdate available: txt v%s (you have v%s). Stop txt and run: txt -update\n", latest, version)
		}()
	}

	// Requests inherit this context, so stopping txt also cancels slow
	// work in progress (like listing a very large folder)
	serverCtx, stopRequests := context.WithCancel(context.Background())
	srv := &http.Server{
		Handler:     handler,
		BaseContext: func(net.Listener) context.Context { return serverCtx },
	}

	// Serve in the background; ErrServerClosed is the normal result of Shutdown
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			fatalf("server: %v", err)
		}
	}()

	// Wait for Ctrl+C (or a termination signal), then stop: give in-flight
	// saves a moment to finish, but don't hang on anything slower
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	stopRequests()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		srv.Close() // still busy after the grace period: force-close
	}
}
