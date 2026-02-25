package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uprockcom/maestro/pkg/version"
)

const (
	// DefaultGitHubReleasesURL is the API endpoint for the latest release.
	DefaultGitHubReleasesURL = "https://api.github.com/repos/uprockcom/maestro/releases/latest"

	// DefaultCheckInterval is how often the daemon checks for updates.
	DefaultCheckInterval = 6 * time.Hour

	// cacheFileName is stored in the maestro config directory.
	cacheFileName = "update-check.json"
)

// Result holds the outcome of an update check.
type Result struct {
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version"`
	UpdateAvail    bool      `json:"update_available"`
	ReleaseURL     string    `json:"release_url"`
	CheckedAt      time.Time `json:"checked_at"`
}

// cacheFile is the on-disk representation.
type cacheFile struct {
	LatestVersion string    `json:"latest_version"`
	ReleaseURL    string    `json:"release_url"`
	CheckedAt     time.Time `json:"checked_at"`
}

// githubRelease is the subset of the GitHub releases API response we need.
type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// Checker manages periodic update checks with caching.
type Checker struct {
	configDir     string
	checkInterval time.Duration
	releasesURL   string // GitHub releases API URL (overridable for tests)
	logInfo       func(string, ...interface{})

	mu     sync.RWMutex
	latest *Result // most recent check result
}

// NewChecker creates an update checker that caches results under configDir.
func NewChecker(configDir string, checkInterval time.Duration, logInfo func(string, ...interface{})) *Checker {
	if checkInterval <= 0 {
		checkInterval = DefaultCheckInterval
	}
	return &Checker{
		configDir:     configDir,
		checkInterval: checkInterval,
		releasesURL:   DefaultGitHubReleasesURL,
		logInfo:       logInfo,
	}
}

// SetReleasesURL overrides the GitHub releases API URL (for testing).
func (c *Checker) SetReleasesURL(url string) {
	c.releasesURL = url
}

// Run starts the periodic check loop. It performs an initial check immediately,
// then rechecks at the configured interval. It blocks until stopChan is closed.
func (c *Checker) Run(stopChan <-chan bool) {
	// Try to load cached result first
	if cached := c.loadCache(); cached != nil {
		result := c.buildResult(cached.LatestVersion, cached.ReleaseURL, cached.CheckedAt)
		c.mu.Lock()
		c.latest = result
		c.mu.Unlock()

		// If the cache is still fresh, don't check immediately
		if time.Since(cached.CheckedAt) < c.checkInterval {
			c.log("Update check: using cached result (latest=%s, checked=%s ago)",
				cached.LatestVersion, time.Since(cached.CheckedAt).Round(time.Minute))
		} else {
			c.checkNow()
		}
	} else {
		c.checkNow()
	}

	ticker := time.NewTicker(c.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.checkNow()
		case <-stopChan:
			return
		}
	}
}

// Latest returns the most recent check result, or nil if no check has completed.
func (c *Checker) Latest() *Result {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// checkNow fetches the latest release from GitHub and updates the cached result.
func (c *Checker) checkNow() {
	result, err := c.fetchLatestRelease()
	if err != nil {
		c.log("Update check failed: %v", err)
		return
	}

	c.mu.Lock()
	c.latest = result
	c.mu.Unlock()

	c.saveCache(&cacheFile{
		LatestVersion: result.LatestVersion,
		ReleaseURL:    result.ReleaseURL,
		CheckedAt:     result.CheckedAt,
	})

	if result.UpdateAvail {
		c.log("Update available: %s -> %s (%s)", result.CurrentVersion, result.LatestVersion, result.ReleaseURL)
	} else {
		c.log("Version %s is up to date", result.CurrentVersion)
	}
}

// fetchLatestRelease queries the GitHub releases API.
func (c *Checker) fetchLatestRelease() (*Result, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("GET", c.releasesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "maestro/"+version.Version)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if release.TagName == "" {
		return nil, fmt.Errorf("empty tag_name in response")
	}

	return c.buildResult(release.TagName, release.HTMLURL, time.Now()), nil
}

// buildResult constructs a Result by comparing the given latest version against
// the running binary's version.
func (c *Checker) buildResult(latestTag, releaseURL string, checkedAt time.Time) *Result {
	current := version.Version
	latest := strings.TrimPrefix(latestTag, "v")

	updateAvail := false
	if !version.IsDevelopment() {
		currentClean := strings.TrimPrefix(current, "v")
		updateAvail = compareSemver(currentClean, latest) < 0
	}

	return &Result{
		CurrentVersion: current,
		LatestVersion:  latest,
		UpdateAvail:    updateAvail,
		ReleaseURL:     releaseURL,
		CheckedAt:      checkedAt,
	}
}

// cachePath returns the full path to the cache file.
func (c *Checker) cachePath() string {
	return filepath.Join(c.configDir, cacheFileName)
}

// loadCache reads the cached check result from disk.
func (c *Checker) loadCache() *cacheFile {
	data, err := os.ReadFile(c.cachePath())
	if err != nil {
		return nil
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil
	}
	if cf.LatestVersion == "" {
		return nil
	}
	return &cf
}

// saveCache writes the check result to disk atomically via temp+rename.
func (c *Checker) saveCache(cf *cacheFile) {
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		c.log("update: failed to marshal cache: %v", err)
		return
	}

	cachePath := c.cachePath()
	dir := filepath.Dir(cachePath)

	if err := os.MkdirAll(dir, 0700); err != nil {
		c.log("update: failed to create cache directory %s: %v", dir, err)
		return
	}

	f, err := os.CreateTemp(dir, ".update-cache-*.tmp")
	if err != nil {
		c.log("update: failed to create temp cache file: %v", err)
		return
	}
	tempName := f.Name()

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tempName)
		c.log("update: failed to write temp cache file: %v", err)
		return
	}

	if err := f.Close(); err != nil {
		os.Remove(tempName)
		c.log("update: failed to close temp cache file: %v", err)
		return
	}

	if err := os.Chmod(tempName, 0600); err != nil {
		os.Remove(tempName)
		c.log("update: failed to set permissions on cache file: %v", err)
		return
	}

	if err := os.Rename(tempName, cachePath); err != nil {
		os.Remove(tempName)
		c.log("update: failed to rename cache file: %v", err)
		return
	}
}

// LoadCachedResult reads the cache file and returns a Result without making
// any network requests. Useful for CLI commands when the daemon isn't running.
func (c *Checker) LoadCachedResult() *Result {
	cached := c.loadCache()
	if cached == nil {
		return nil
	}
	return c.buildResult(cached.LatestVersion, cached.ReleaseURL, cached.CheckedAt)
}

func (c *Checker) log(format string, args ...interface{}) {
	if c.logInfo != nil {
		c.logInfo(format, args...)
	}
}

// compareSemver compares two semver strings (without "v" prefix).
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Pre-release versions (e.g., "1.0.0-rc1") are considered lower than the
// corresponding plain version ("1.0.0").
func compareSemver(a, b string) int {
	aHasPre := strings.Contains(a, "-")
	bHasPre := strings.Contains(b, "-")

	aParts := parseSemverParts(a)
	bParts := parseSemverParts(b)

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}

	// Same major.minor.patch — pre-release is lower than stable
	if aHasPre && !bHasPre {
		return -1
	}
	if !aHasPre && bHasPre {
		return 1
	}

	return 0
}

// parseSemverParts extracts [major, minor, patch] from a version string.
// Handles formats like "1.2.3", "1.2.3-rc1", "1.2".
func parseSemverParts(v string) [3]int {
	var parts [3]int
	// Strip pre-release suffix (everything after first hyphen)
	if idx := strings.Index(v, "-"); idx >= 0 {
		v = v[:idx]
	}
	segments := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(segments); i++ {
		n, _ := strconv.Atoi(segments[i])
		parts[i] = n
	}
	return parts
}

// FormatUpdateWarning returns a formatted warning string for CLI display,
// or empty string if no update is available.
func FormatUpdateWarning(result *Result) string {
	if result == nil || !result.UpdateAvail {
		return ""
	}

	return fmt.Sprintf(
		"\n⚠️  Update available: %s → %s\n"+
			"   Run: brew upgrade maestro  (or download from GitHub)\n"+
			"   %s\n",
		result.CurrentVersion, result.LatestVersion, result.ReleaseURL,
	)
}
