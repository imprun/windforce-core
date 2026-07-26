package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestWebUIServedWithoutAPIAuth(t *testing.T) {
	handler := New(Config{AdminToken: "secret"})

	root := httptest.NewRecorder()
	handler.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	if root.Code != http.StatusFound {
		t.Fatalf("root status = %d, want %d", root.Code, http.StatusFound)
	}
	if got := root.Header().Get("Location"); got != "/ui/" {
		t.Fatalf("root location = %q, want /ui/", got)
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("ui status = %d, want %d", page.Code, http.StatusOK)
	}
	if !strings.Contains(page.Body.String(), "windforce-core") {
		t.Fatalf("ui page did not contain product name")
	}

	assetPath := regexp.MustCompile(`src="/ui/([^"]+\.js)"`).FindStringSubmatch(page.Body.String())
	if len(assetPath) != 2 {
		t.Fatalf("ui page did not reference a script asset")
	}
	script := httptest.NewRecorder()
	handler.ServeHTTP(script, httptest.NewRequest(http.MethodGet, "/ui/"+assetPath[1], nil))
	if script.Code != http.StatusOK {
		t.Fatalf("ui script status = %d, want %d", script.Code, http.StatusOK)
	}

	// Client-side SPA routes fall back to index.html.
	deepLink := httptest.NewRecorder()
	handler.ServeHTTP(deepLink, httptest.NewRequest(http.MethodGet, "/ui/jobs/some-job-id", nil))
	if deepLink.Code != http.StatusOK {
		t.Fatalf("ui deep link status = %d, want %d", deepLink.Code, http.StatusOK)
	}
	if !strings.Contains(deepLink.Body.String(), "windforce-core") {
		t.Fatalf("ui deep link did not serve the SPA index page")
	}

	// Missing hashed assets must stay 404 so stale browsers do not cache
	// index.html under an old bundle URL.
	missingAsset := httptest.NewRecorder()
	handler.ServeHTTP(missingAsset, httptest.NewRequest(http.MethodGet, "/ui/assets/index-stalehash.js", nil))
	if missingAsset.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want %d", missingAsset.Code, http.StatusNotFound)
	}

	api := httptest.NewRecorder()
	handler.ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/api/w/default/apps", nil))
	if api.Code != http.StatusUnauthorized {
		t.Fatalf("api status = %d, want %d", api.Code, http.StatusUnauthorized)
	}
}

func TestWebUIExposesValidatedHostConsoleConfig(t *testing.T) {
	handler := New(Config{
		UIHostURL:   "https://portal.example.test/console",
		UIHostLabel: "Back to operations portal",
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/ui/config.json", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("config status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q, want no-store", got)
	}
	var config struct {
		HostConsole *struct {
			URL   string `json:"url"`
			Label string `json:"label"`
		} `json:"host_console"`
	}
	if err := json.NewDecoder(response.Body).Decode(&config); err != nil {
		t.Fatal(err)
	}
	if config.HostConsole == nil ||
		config.HostConsole.URL != "https://portal.example.test/console" ||
		config.HostConsole.Label != "Back to operations portal" {
		t.Fatalf("host console config = %#v", config.HostConsole)
	}

	invalid := New(Config{UIHostURL: "javascript:alert(1)"})
	response = httptest.NewRecorder()
	invalid.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/ui/config.json", nil),
	)
	config = struct {
		HostConsole *struct {
			URL   string `json:"url"`
			Label string `json:"label"`
		} `json:"host_console"`
	}{}
	if err := json.NewDecoder(response.Body).Decode(&config); err != nil {
		t.Fatal(err)
	}
	if config.HostConsole != nil {
		t.Fatalf("invalid host console was exposed: %#v", config.HostConsole)
	}
}
