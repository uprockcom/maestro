package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/uprockcom/maestro/pkg/api"
	"github.com/uprockcom/maestro/pkg/container"
)

const cacheTTL = 35 * time.Second

// ContainerCache provides a lazy, singleflight-coalesced cache of container state.
// Only ONE refresh call runs at a time — concurrent requests share results.
type ContainerCache struct {
	prefix string

	// refreshFn fetches all containers. Defaults to container.GetAllContainers.
	// Replaceable in tests to inject a controllable function.
	refreshFn func(prefix string) ([]container.Info, error)

	mu        sync.Mutex
	data      []api.ContainerInfo
	stateHash string
	updatedAt time.Time
	lastErr   error // set when refresh fails, so waiters see the error too

	// Singleflight: only one refresh runs at a time. Waiters share the result.
	refreshCh  chan struct{} // closed when the current refresh completes
	refreshing bool
}

// NewContainerCache creates a new container cache for the given prefix.
func NewContainerCache(prefix string) *ContainerCache {
	return &ContainerCache{
		prefix:    prefix,
		refreshFn: container.GetAllContainers,
	}
}

// Get returns the cached container list and state hash.
// If the cache has any data (even stale), returns it immediately and triggers
// a background refresh so the next call gets fresher data.
// Only blocks on the very first call when there is no data at all.
func (c *ContainerCache) Get() ([]api.ContainerInfo, string, error) {
	c.mu.Lock()
	if c.data != nil {
		data := c.data
		hash := c.stateHash
		fresh := time.Since(c.updatedAt) < cacheTTL
		c.mu.Unlock()
		if !fresh {
			// Return stale data immediately, refresh in background
			go c.refreshAndGet() //nolint:errcheck
		}
		return data, hash, nil
	}
	c.mu.Unlock()

	// No data yet (first call ever) — must block until we have something
	return c.refreshAndGet()
}

// ForceRefresh forces an immediate cache refresh, ignoring TTL.
func (c *ContainerCache) ForceRefresh() ([]api.ContainerInfo, string, error) {
	return c.forceRefresh()
}

// ValidateStateHash checks if the given hash matches the current state.
// An empty hash skips validation (used for direct CLI commands where
// the user specified a container name without listing first).
func (c *ContainerCache) ValidateStateHash(hash string) bool {
	if hash == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stateHash == hash
}

// CachedAt returns the time the cache was last updated (for API responses).
func (c *ContainerCache) CachedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.updatedAt
}

// refreshAndGet coalesces concurrent refresh requests.
// Re-checks the TTL after acquiring the lock to avoid redundant refreshes
// when another goroutine completed a refresh between Get() and here.
func (c *ContainerCache) refreshAndGet() ([]api.ContainerInfo, string, error) {
	c.mu.Lock()

	// Re-check TTL: another goroutine may have completed a refresh
	// between our Get() check and acquiring this lock.
	if time.Since(c.updatedAt) < cacheTTL && c.data != nil {
		data := c.data
		hash := c.stateHash
		c.mu.Unlock()
		return data, hash, nil
	}

	return c.doRefreshLocked()
}

// forceRefresh bypasses TTL and always triggers a refresh.
func (c *ContainerCache) forceRefresh() ([]api.ContainerInfo, string, error) {
	c.mu.Lock()
	return c.doRefreshLocked()
}

// doRefreshLocked performs the actual refresh. Caller must hold c.mu.
// Either becomes the leader (does the Docker call) or waits for an in-flight refresh.
func (c *ContainerCache) doRefreshLocked() ([]api.ContainerInfo, string, error) {
	if c.refreshing {
		// Another goroutine is refreshing — wait for it
		ch := c.refreshCh
		c.mu.Unlock()
		<-ch // block until refresh completes
		c.mu.Lock()
		data := c.data
		hash := c.stateHash
		err := c.lastErr
		c.mu.Unlock()
		return data, hash, err
	}

	// We are the leader — mark refreshing and create the signal channel
	c.refreshing = true
	c.refreshCh = make(chan struct{})
	c.mu.Unlock()

	// Do the actual Docker call (this is the expensive part, 1-3s)
	containers, err := c.refreshFn(c.prefix)

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		// Store error so waiters see it too, then signal them
		c.lastErr = err
		c.refreshing = false
		close(c.refreshCh)
		return nil, "", err
	}

	// Convert to API types and compute hash
	apiContainers := toAPIContainers(containers)
	hash := computeStateHash(apiContainers)

	c.data = apiContainers
	c.stateHash = hash
	c.updatedAt = time.Now()
	c.lastErr = nil // clear any previous error
	c.refreshing = false
	close(c.refreshCh) // wake up all waiters

	return c.data, c.stateHash, nil
}

// toAPIContainers converts container.Info slice to api.ContainerInfo slice.
func toAPIContainers(infos []container.Info) []api.ContainerInfo {
	result := make([]api.ContainerInfo, len(infos))
	for i, c := range infos {
		result[i] = api.ContainerInfo{
			Name:          c.Name,
			ShortName:     c.ShortName,
			Status:        c.Status,
			StatusDetails: c.StatusDetails,
			Branch:        c.Branch,
			AgentState:    c.AgentState,
			IsDormant:     c.IsDormant,
			HasWeb:        c.HasWeb,
			AuthStatus:    c.AuthStatus,
			LastActivity:  c.LastActivity,
			GitStatus:     c.GitStatus,
			CreatedAt:     c.CreatedAt,
			CurrentTask:   c.CurrentTask,
			TaskProgress:  c.TaskProgress,
			Contacts:      c.Contacts,
		}
	}
	return result
}

// computeStateHash produces a deterministic hash of the container list.
// Used for optimistic concurrency — clients send the hash back with action
// requests, and the daemon rejects if state has changed.
//
// The hash covers: sorted container names + their statuses + dormancy.
// This means: a new container appearing, a container stopping, or a container
// status changing will all invalidate the hash.
func computeStateHash(containers []api.ContainerInfo) string {
	// Sort by name for determinism
	sorted := make([]api.ContainerInfo, len(containers))
	copy(sorted, containers)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	h := sha256.New()
	for _, c := range sorted {
		fmt.Fprintf(h, "%s:%s:%v\n", c.Name, c.Status, c.IsDormant)
	}
	return hex.EncodeToString(h.Sum(nil))[:16] // 16 hex chars = 64 bits, plenty
}
