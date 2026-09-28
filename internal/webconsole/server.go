package webconsole

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sessionTokenBytes = 32

//go:embed all:ui
var embeddedUI embed.FS

// Options configures one local Web console process session.
type Options struct {
	Origin      string
	LibraryPath string
	HomeDir     string
	DataRoot    string
	ProjectPath string
	TokenSource io.Reader
	Now         func() time.Time
	Assets      fs.FS
}

// Server is the authenticated HTTP delivery adapter for one local project.
type Server struct {
	mu                         sync.RWMutex
	plansMu                    sync.Mutex
	operationMu                sync.Mutex
	token                      string
	origin                     string
	assets                     fs.FS
	libraryPath                string
	homeDir                    string
	dataRoot                   string
	project                    *registeredProject
	plans                      map[string]*storedPlan
	now                        func() time.Time
	listen                     func(string, string) (net.Listener, error)
	beforeJournalDirectorySync func() error
}

// New creates an authenticated local console session. A supplied Origin is a
// test seam; production code learns its exact origin from Listen.
func New(options Options) (*Server, error) {
	tokenSource := options.TokenSource
	if tokenSource == nil {
		tokenSource = rand.Reader
	}
	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := io.ReadFull(tokenSource, tokenBytes); err != nil {
		return nil, fmt.Errorf("create local console session token: %w", err)
	}
	if options.Origin != "" {
		if err := validateLoopbackOrigin(options.Origin); err != nil {
			return nil, err
		}
	}
	assets := options.Assets
	if assets == nil {
		var err error
		assets, err = fs.Sub(embeddedUI, "ui")
		if err != nil {
			return nil, fmt.Errorf("load embedded console assets: %w", err)
		}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	server := &Server{
		token:       hex.EncodeToString(tokenBytes),
		origin:      options.Origin,
		assets:      assets,
		libraryPath: options.LibraryPath,
		homeDir:     options.HomeDir,
		dataRoot:    options.DataRoot,
		now:         now,
		listen:      net.Listen,
		plans:       make(map[string]*storedPlan),
	}
	if options.ProjectPath != "" {
		if _, err := server.registerProject(options.ProjectPath); err != nil {
			return nil, err
		}
	}
	return server, nil
}

// Listen opens a TCP4 listener on 127.0.0.1 only and records its exact origin.
func (s *Server) Listen(port int) (net.Listener, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("port must be between 0 and 65535")
	}
	listener, err := s.listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("listen on local console: %w", err)
	}
	origin := "http://" + listener.Addr().String()
	s.mu.Lock()
	s.origin = origin
	s.mu.Unlock()
	return listener, nil
}

// EntryURL returns a same-origin URL with the session token in its fragment.
// Fragments are not sent to the HTTP server or included in referrer headers.
func (s *Server) EntryURL() string {
	s.mu.RLock()
	origin := s.origin
	s.mu.RUnlock()
	if origin == "" {
		return ""
	}
	return origin + "/#session=" + s.token
}

// ServeHTTP serves embedded console assets and authenticated versioned APIs.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if !s.authorizeAPI(w, r) {
			return
		}
		s.handleAPI(w, r)
		return
	}
	s.serveUI(w, r)
}

func (s *Server) authorizeAPI(w http.ResponseWriter, r *http.Request) bool {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "A valid console session token is required.")
		return false
	}

	s.mu.RLock()
	origin := s.origin
	s.mu.RUnlock()
	expected, err := url.Parse(origin)
	if err != nil || expected.Host == "" || r.Host != expected.Host {
		writeAPIError(w, http.StatusForbidden, "untrusted_host", "The request must use the current loopback console origin.")
		return false
	}
	requestOrigin := r.Header.Get("Origin")
	if requestOrigin != "" && requestOrigin != origin {
		writeAPIError(w, http.StatusForbidden, "untrusted_origin", "The request origin is not allowed.")
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && requestOrigin != origin {
		writeAPIError(w, http.StatusForbidden, "origin_required", "State-changing requests must come from the console origin.")
		return false
	}
	if site := strings.ToLower(r.Header.Get("Sec-Fetch-Site")); site == "cross-site" {
		writeAPIError(w, http.StatusForbidden, "cross_site_request", "Cross-site requests are not allowed.")
		return false
	}
	return true
}

func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "The console only serves read requests on this route.")
		return
	}
	assetPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if assetPath == "." || assetPath == "" {
		assetPath = "index.html"
	}
	if fs.ValidPath(assetPath) {
		if info, err := fs.Stat(s.assets, assetPath); err == nil && !info.IsDir() {
			s.serveAsset(w, r, assetPath)
			return
		}
	}
	if path.Ext(assetPath) != "" {
		http.NotFound(w, r)
		return
	}
	s.serveAsset(w, r, "index.html")
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	contents, err := fs.ReadFile(s.assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(contents))
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
}

func validateLoopbackOrigin(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return errors.New("console origin must be an HTTP IPv4 loopback origin with a port")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return errors.New("console origin must use a valid TCP port")
	}
	return nil
}

type apiError struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: errorBody{Code: code, Message: message}})
}
