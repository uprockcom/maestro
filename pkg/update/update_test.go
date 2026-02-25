package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.2.3", "1.2.4", -1},
		{"1.2.3", "1.3.0", -1},
		{"0.9.9", "1.0.0", -1},
		// Pre-release is lower than the same stable version
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"1.0.0-rc1", "1.0.0-rc2", 0}, // both pre-release, same base
		// Pre-release of a higher version is still higher than lower stable
		{"2.0.0-rc1", "1.9.9", 1},
	}

	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseSemverParts(t *testing.T) {
	tests := []struct {
		input string
		want  [3]int
	}{
		{"1.2.3", [3]int{1, 2, 3}},
		{"0.1.0", [3]int{0, 1, 0}},
		{"10.20.30", [3]int{10, 20, 30}},
		{"1.2", [3]int{1, 2, 0}},
		{"1", [3]int{1, 0, 0}},
		{"1.2.3-rc1", [3]int{1, 2, 3}},
		{"", [3]int{0, 0, 0}},
	}

	for _, tt := range tests {
		got := parseSemverParts(tt.input)
		if got != tt.want {
			t.Errorf("parseSemverParts(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := NewChecker(dir, DefaultCheckInterval, nil)

	cf := &cacheFile{
		LatestVersion: "1.5.0",
		ReleaseURL:    "https://github.com/uprockcom/maestro/releases/tag/v1.5.0",
		CheckedAt:     time.Now().Truncate(time.Second),
	}

	c.saveCache(cf)

	loaded := c.loadCache()
	if loaded == nil {
		t.Fatal("loadCache returned nil")
	}
	if loaded.LatestVersion != cf.LatestVersion {
		t.Errorf("LatestVersion = %q, want %q", loaded.LatestVersion, cf.LatestVersion)
	}
	if loaded.ReleaseURL != cf.ReleaseURL {
		t.Errorf("ReleaseURL = %q, want %q", loaded.ReleaseURL, cf.ReleaseURL)
	}

	// Verify JSON file exists with correct permissions
	cachePath := filepath.Join(dir, cacheFileName)
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("cache file permissions = %o, want 0600", perm)
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed cacheFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.LatestVersion != "1.5.0" {
		t.Errorf("parsed LatestVersion = %q, want %q", parsed.LatestVersion, "1.5.0")
	}
}

func TestCacheLoadMissing(t *testing.T) {
	dir := t.TempDir()
	c := NewChecker(dir, DefaultCheckInterval, nil)

	if loaded := c.loadCache(); loaded != nil {
		t.Errorf("expected nil for missing cache, got %+v", loaded)
	}
}

func TestFetchLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if accept := r.Header.Get("Accept"); accept != "application/vnd.github+json" {
			t.Errorf("Accept header = %q, want %q", accept, "application/vnd.github+json")
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "maestro/") {
			t.Errorf("User-Agent header = %q, want prefix %q", ua, "maestro/")
		}

		resp := githubRelease{
			TagName: "v2.0.0",
			HTMLURL: "https://github.com/uprockcom/maestro/releases/tag/v2.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewChecker(t.TempDir(), DefaultCheckInterval, nil)
	c.SetReleasesURL(server.URL)

	result, err := c.fetchLatestRelease()
	if err != nil {
		t.Fatal(err)
	}
	if result.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q", result.LatestVersion, "2.0.0")
	}
	if result.ReleaseURL != "https://github.com/uprockcom/maestro/releases/tag/v2.0.0" {
		t.Errorf("ReleaseURL = %q, want GitHub URL", result.ReleaseURL)
	}
}

func TestFetchLatestRelease_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewChecker(t.TempDir(), DefaultCheckInterval, nil)
	c.SetReleasesURL(server.URL)

	_, err := c.fetchLatestRelease()
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestFetchLatestRelease_EmptyTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := githubRelease{TagName: "", HTMLURL: ""}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewChecker(t.TempDir(), DefaultCheckInterval, nil)
	c.SetReleasesURL(server.URL)

	_, err := c.fetchLatestRelease()
	if err == nil {
		t.Fatal("expected error for empty tag_name")
	}
}

func TestFetchLatestRelease_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{invalid json"))
	}))
	defer server.Close()

	c := NewChecker(t.TempDir(), DefaultCheckInterval, nil)
	c.SetReleasesURL(server.URL)

	_, err := c.fetchLatestRelease()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFormatUpdateWarning(t *testing.T) {
	// No result
	if msg := FormatUpdateWarning(nil); msg != "" {
		t.Errorf("expected empty for nil, got %q", msg)
	}

	// No update available
	result := &Result{UpdateAvail: false}
	if msg := FormatUpdateWarning(result); msg != "" {
		t.Errorf("expected empty for no update, got %q", msg)
	}

	// Update available
	result = &Result{
		CurrentVersion: "1.0.0",
		LatestVersion:  "2.0.0",
		UpdateAvail:    true,
		ReleaseURL:     "https://github.com/uprockcom/maestro/releases/tag/v2.0.0",
	}
	msg := FormatUpdateWarning(result)
	if msg == "" {
		t.Error("expected non-empty warning")
	}
	if !strings.Contains(msg, "1.0.0") || !strings.Contains(msg, "2.0.0") {
		t.Errorf("warning should contain versions, got: %s", msg)
	}
	if !strings.Contains(msg, "brew upgrade") {
		t.Errorf("warning should contain upgrade instructions, got: %s", msg)
	}
}
