package webconsole

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAPIRoutesRequireSessionToken(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/not-yet-routed", nil)
	req.Host = "127.0.0.1:8787"
	response := httptest.NewRecorder()

	server.ServeHTTP(response, req)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if strings.Contains(response.Body.String(), "library") {
		t.Fatalf("unauthenticated response exposed application data: %s", response.Body.String())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want no cross-origin grant", got)
	}
}

func TestAPIRejectsUntrustedOriginEvenWithToken(t *testing.T) {
	server := newTestServer(t)
	req := authenticatedRequest(t, server, http.MethodGet, "/api/v1/not-yet-routed")
	req.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()

	server.ServeHTTP(response, req)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want no cross-origin grant", got)
	}
}

func TestAPIRequiresOriginForMutationAndRejectsCrossSiteFetch(t *testing.T) {
	server := newTestServer(t)
	for _, tc := range []struct {
		name   string
		origin string
		fetch  string
	}{
		{name: "missing origin"},
		{name: "cross-site fetch metadata", origin: "http://127.0.0.1:8787", fetch: "cross-site"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := authenticatedRequest(t, server, http.MethodPost, "/api/v1/not-yet-routed")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.fetch != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetch)
			}
			response := httptest.NewRecorder()

			server.ServeHTTP(response, req)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
}

func TestEntryURLKeepsTokenInFragment(t *testing.T) {
	server := newTestServer(t)
	entry, err := url.Parse(server.EntryURL())
	if err != nil {
		t.Fatal(err)
	}
	if entry.Scheme != "http" || entry.Host != "127.0.0.1:8787" || entry.Path != "/" {
		t.Fatalf("entry URL = %q, want loopback root URL", server.EntryURL())
	}
	if entry.RawQuery != "" || !strings.HasPrefix(entry.Fragment, "session=") || len(strings.TrimPrefix(entry.Fragment, "session=")) != 64 {
		t.Fatalf("entry URL = %q, want 32-byte session token in fragment only", server.EntryURL())
	}
}

func TestListenBindsOnlyIPv4Loopback(t *testing.T) {
	server := newTestServer(t)
	var gotNetwork, gotAddress string
	server.listen = func(network, address string) (net.Listener, error) {
		gotNetwork, gotAddress = network, address
		return testListener{address: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 51423}}, nil
	}
	listener, err := server.Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() || addr.IP.To4() == nil {
		t.Fatalf("listener address = %v, want IPv4 loopback", listener.Addr())
	}
	if gotNetwork != "tcp4" || gotAddress != "127.0.0.1:0" {
		t.Fatalf("Listen called net.Listen(%q, %q), want tcp4 127.0.0.1:0", gotNetwork, gotAddress)
	}
	entry, err := url.Parse(server.EntryURL())
	if err != nil || entry.Host != listener.Addr().String() {
		t.Fatalf("entry URL = %q, listener = %v, err = %v", server.EntryURL(), listener.Addr(), err)
	}
}

func TestConsoleAssetsHaveSecurityHeaders(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, req)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "console shell") {
		t.Fatalf("asset response = %d %q", response.Code, response.Body.String())
	}
	for header, want := range map[string]string{
		"Cache-Control":           "no-store",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'self'",
	} {
		if got := response.Header().Get(header); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want to include %q", header, got, want)
		}
	}
}

func TestEmbeddedAssetsLoadFromDeepSPARoutes(t *testing.T) {
	server, err := New(Options{Origin: "http://127.0.0.1:8787", LibraryPath: t.TempDir(), HomeDir: t.TempDir(), DataRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	route := httptest.NewRecorder()
	server.ServeHTTP(route, httptest.NewRequest(http.MethodGet, "/subagents/reviewer", nil))
	if route.Code != http.StatusOK {
		t.Fatalf("deep route status = %d, want 200", route.Code)
	}
	markup := route.Body.String()
	assetsChecked := 0
	for _, attribute := range []string{`src="`, `href="`} {
		for remaining := markup; ; {
			index := strings.Index(remaining, attribute)
			if index < 0 {
				break
			}
			remaining = remaining[index+len(attribute):]
			end := strings.IndexByte(remaining, '"')
			if end < 0 {
				break
			}
			asset := remaining[:end]
			remaining = remaining[end+1:]
			if !strings.HasPrefix(asset, "/assets/") {
				continue
			}
			assetResponse := httptest.NewRecorder()
			server.ServeHTTP(assetResponse, httptest.NewRequest(http.MethodGet, asset, nil))
			if assetResponse.Code != http.StatusOK {
				t.Errorf("deep route asset %q status = %d, want 200", asset, assetResponse.Code)
			}
			assetsChecked++
		}
	}
	if assetsChecked < 2 {
		t.Fatalf("deep route markup exposed %d root assets, want script and stylesheet: %s", assetsChecked, markup)
	}
}

func TestProjectRegistrationCanonicalizesPathAndReturnsSessionID(t *testing.T) {
	server := newTestServer(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "project-link")
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, server, http.MethodPost, "/api/v1/projects/inspect", `{"path":"`+link+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var first projectEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Project.ID == "" || first.Project.Path != canonical || first.Project.Name != "project" {
		t.Fatalf("registered project = %#v", first.Project)
	}

	response = request(t, server, http.MethodPost, "/api/v1/projects/inspect", `{"path":"`+project+`"}`)
	var second projectEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || second.Project.ID != first.Project.ID {
		t.Fatalf("same project registration = %d %#v, want stable ID %q", response.Code, second.Project, first.Project.ID)
	}
}

func TestProjectRegistrationRejectsInvalidAndDifferentProjects(t *testing.T) {
	server := newTestServer(t)
	root := t.TempDir()
	file := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "missing"), file} {
		response := request(t, server, http.MethodPost, "/api/v1/projects/inspect", `{"path":"`+path+`"}`)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("register %q status = %d, body = %s, want 400", path, response.Code, response.Body.String())
		}
	}

	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.Mkdir(first, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(second, 0o755); err != nil {
		t.Fatal(err)
	}
	response := request(t, server, http.MethodPost, "/api/v1/projects/inspect", `{"path":"`+first+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("register first project status = %d, body = %s", response.Code, response.Body.String())
	}
	response = request(t, server, http.MethodPost, "/api/v1/projects/inspect", `{"path":"`+second+`"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("register second project status = %d, body = %s, want 409", response.Code, response.Body.String())
	}
}

func TestProjectRegistrationRejectsUnknownAndOversizedJSON(t *testing.T) {
	server := newTestServer(t)
	tooLarge := `{"path":"` + strings.Repeat("x", 70*1024) + `"}`
	for _, tc := range []struct {
		body string
		want int
	}{
		{body: `{"path":"/tmp","other":"value"}`, want: http.StatusBadRequest},
		{body: tooLarge, want: http.StatusRequestEntityTooLarge},
	} {
		response := request(t, server, http.MethodPost, "/api/v1/projects/inspect", tc.body)
		if response.Code != tc.want {
			t.Fatalf("request body length %d status = %d, body = %s, want %d", len(tc.body), response.Code, response.Body.String(), tc.want)
		}
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := New(Options{
		Origin:      "http://127.0.0.1:8787",
		LibraryPath: t.TempDir(),
		HomeDir:     t.TempDir(),
		DataRoot:    t.TempDir(),
		TokenSource: bytes.NewReader(make([]byte, 32)),
		Assets: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("console shell")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func authenticatedRequest(t *testing.T, server *Server, method, path string) *http.Request {
	t.Helper()
	entry, err := url.Parse(server.EntryURL())
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(entry.Fragment, "session=")
	req := httptest.NewRequest(method, path, nil)
	req.Host = entry.Host
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func request(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := authenticatedRequest(t, server, method, path)
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(strings.NewReader(body))
	req.ContentLength = int64(len(body))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	return response
}

type projectEnvelope struct {
	Project struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"project"`
}

type testListener struct{ address net.Addr }

func (listener testListener) Accept() (net.Conn, error) {
	return nil, errors.New("unused test listener")
}
func (listener testListener) Close() error   { return nil }
func (listener testListener) Addr() net.Addr { return listener.address }
