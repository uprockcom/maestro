package daemon

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uprockcom/maestro/pkg/api"
	"github.com/uprockcom/maestro/pkg/container"
)

func TestComputeStateHash_Deterministic(t *testing.T) {
	containers := []api.ContainerInfo{
		{Name: "maestro-a-1", Status: "running", IsDormant: false},
		{Name: "maestro-b-1", Status: "exited", IsDormant: true},
	}

	hash1 := computeStateHash(containers)
	hash2 := computeStateHash(containers)
	if hash1 != hash2 {
		t.Errorf("hash should be deterministic: %s != %s", hash1, hash2)
	}
}

func TestComputeStateHash_OrderIndependent(t *testing.T) {
	containers1 := []api.ContainerInfo{
		{Name: "maestro-a-1", Status: "running"},
		{Name: "maestro-b-1", Status: "exited"},
	}
	containers2 := []api.ContainerInfo{
		{Name: "maestro-b-1", Status: "exited"},
		{Name: "maestro-a-1", Status: "running"},
	}

	hash1 := computeStateHash(containers1)
	hash2 := computeStateHash(containers2)
	if hash1 != hash2 {
		t.Errorf("hash should be order-independent: %s != %s", hash1, hash2)
	}
}

func TestComputeStateHash_ChangesOnStatusChange(t *testing.T) {
	before := []api.ContainerInfo{
		{Name: "maestro-a-1", Status: "running"},
	}
	after := []api.ContainerInfo{
		{Name: "maestro-a-1", Status: "exited"},
	}

	hashBefore := computeStateHash(before)
	hashAfter := computeStateHash(after)
	if hashBefore == hashAfter {
		t.Error("hash should change when status changes")
	}
}

func TestComputeStateHash_ChangesOnDormantChange(t *testing.T) {
	before := []api.ContainerInfo{
		{Name: "maestro-a-1", Status: "running", IsDormant: false},
	}
	after := []api.ContainerInfo{
		{Name: "maestro-a-1", Status: "running", IsDormant: true},
	}

	hashBefore := computeStateHash(before)
	hashAfter := computeStateHash(after)
	if hashBefore == hashAfter {
		t.Error("hash should change when dormant state changes")
	}
}

func TestComputeStateHash_StableOnAgentStateChange(t *testing.T) {
	before := []api.ContainerInfo{
		{Name: "maestro-a-1", Status: "running", AgentState: "idle"},
	}
	after := []api.ContainerInfo{
		{Name: "maestro-a-1", Status: "running", AgentState: "active"},
	}

	hashBefore := computeStateHash(before)
	hashAfter := computeStateHash(after)
	if hashBefore != hashAfter {
		t.Error("hash should NOT change when only agent state changes")
	}
}

func TestComputeStateHash_EmptyList(t *testing.T) {
	hash1 := computeStateHash(nil)
	hash2 := computeStateHash([]api.ContainerInfo{})
	if hash1 != hash2 {
		t.Error("nil and empty should produce same hash")
	}
	if hash1 == "" {
		t.Error("hash should not be empty even for empty list")
	}
	if len(hash1) != 16 {
		t.Errorf("hash should be 16 chars, got %d", len(hash1))
	}
}

func TestContainerCache_ValidateStateHash(t *testing.T) {
	cache := NewContainerCache("maestro-")

	// Manually set cache state
	cache.mu.Lock()
	cache.data = []api.ContainerInfo{{Name: "maestro-test-1", Status: "running"}}
	cache.stateHash = "abc123"
	cache.updatedAt = time.Now()
	cache.mu.Unlock()

	if !cache.ValidateStateHash("abc123") {
		t.Error("should validate correct hash")
	}
	if cache.ValidateStateHash("wrong") {
		t.Error("should reject wrong hash")
	}
	if !cache.ValidateStateHash("") {
		t.Error("empty hash should skip validation (direct CLI commands)")
	}
}

func TestContainerCache_CachedAt(t *testing.T) {
	cache := NewContainerCache("maestro-")

	// Initially zero
	if !cache.CachedAt().IsZero() {
		t.Error("CachedAt should be zero initially")
	}

	// After setting data
	now := time.Now()
	cache.mu.Lock()
	cache.data = []api.ContainerInfo{}
	cache.updatedAt = now
	cache.mu.Unlock()

	if !cache.CachedAt().Equal(now) {
		t.Errorf("CachedAt should be %v, got %v", now, cache.CachedAt())
	}
}

// newTestCache creates a cache with an injectable refresh function.
func newTestCache(fn func(prefix string) ([]container.Info, error)) *ContainerCache {
	cache := NewContainerCache("test-")
	cache.refreshFn = fn
	return cache
}

func TestContainerCache_Get_FreshCacheHit(t *testing.T) {
	var callCount atomic.Int32
	cache := newTestCache(func(prefix string) ([]container.Info, error) {
		callCount.Add(1)
		return []container.Info{{Name: "test-1", Status: "running"}}, nil
	})

	// First call: cache is empty, triggers refresh
	data1, hash1, err := cache.Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data1) != 1 || data1[0].Name != "test-1" {
		t.Errorf("unexpected data: %v", data1)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 refresh call, got %d", callCount.Load())
	}

	// Second call within TTL: should use cache (no refresh)
	data2, hash2, err := cache.Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("hash should be same on cache hit: %s != %s", hash1, hash2)
	}
	if len(data2) != 1 {
		t.Errorf("unexpected data on cache hit: %v", data2)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected still 1 refresh call (cache hit), got %d", callCount.Load())
	}
}

func TestContainerCache_Get_StaleTriggersRefresh(t *testing.T) {
	var callCount atomic.Int32
	cache := newTestCache(func(prefix string) ([]container.Info, error) {
		callCount.Add(1)
		return []container.Info{{Name: "test-1", Status: "running"}}, nil
	})

	// First call
	cache.Get()
	if callCount.Load() != 1 {
		t.Fatalf("expected 1 call after first Get, got %d", callCount.Load())
	}

	// Make cache stale
	cache.mu.Lock()
	cache.updatedAt = time.Now().Add(-10 * time.Second)
	cache.mu.Unlock()

	// Second call: cache is stale, should trigger refresh
	_, _, err := cache.Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount.Load() != 2 {
		t.Errorf("expected 2 refresh calls after stale Get, got %d", callCount.Load())
	}
}

func TestContainerCache_ForceRefresh_BypassesTTL(t *testing.T) {
	var callCount atomic.Int32
	cache := newTestCache(func(prefix string) ([]container.Info, error) {
		callCount.Add(1)
		return []container.Info{{Name: "test-1", Status: "running"}}, nil
	})

	// Populate cache
	cache.Get()
	if callCount.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", callCount.Load())
	}

	// ForceRefresh should bypass TTL
	_, _, err := cache.ForceRefresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount.Load() != 2 {
		t.Errorf("ForceRefresh should have triggered a refresh, got %d calls", callCount.Load())
	}
}

func TestContainerCache_Singleflight(t *testing.T) {
	var callCount atomic.Int32
	blocker := make(chan struct{})

	cache := newTestCache(func(prefix string) ([]container.Info, error) {
		callCount.Add(1)
		<-blocker // block until test releases
		return []container.Info{{Name: "test-1", Status: "running"}}, nil
	})

	// Launch 10 concurrent Get() calls
	var wg sync.WaitGroup
	results := make([]string, 10)
	errs := make([]error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, hash, err := cache.Get()
			results[idx] = hash
			errs[idx] = err
		}(i)
	}

	// Let goroutines pile up on the blocked refresh
	time.Sleep(50 * time.Millisecond)

	// Release the refresh — all goroutines should share the result
	close(blocker)
	wg.Wait()

	// Exactly 1 refresh call (singleflight coalesced)
	if callCount.Load() != 1 {
		t.Errorf("expected exactly 1 Docker call (singleflight), got %d", callCount.Load())
	}

	// All got the same hash, no errors
	for i := range results {
		if errs[i] != nil {
			t.Errorf("goroutine %d got error: %v", i, errs[i])
		}
		if results[i] != results[0] {
			t.Errorf("goroutine %d got different hash %q vs %q", i, results[i], results[0])
		}
	}
}

func TestContainerCache_Singleflight_ErrorPropagation(t *testing.T) {
	blocker := make(chan struct{})
	testErr := fmt.Errorf("docker not available")

	cache := newTestCache(func(prefix string) ([]container.Info, error) {
		<-blocker
		return nil, testErr
	})

	// Launch 5 concurrent Get() calls
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _, err := cache.Get()
			errs[idx] = err
		}(i)
	}

	time.Sleep(50 * time.Millisecond)
	close(blocker)
	wg.Wait()

	// All goroutines should see the error
	for i, err := range errs {
		if err == nil {
			t.Errorf("goroutine %d: expected error, got nil", i)
		}
	}
}

func TestContainerCache_TTLRecheck_PreventsRedundantRefresh(t *testing.T) {
	var callCount atomic.Int32
	cache := newTestCache(func(prefix string) ([]container.Info, error) {
		callCount.Add(1)
		return []container.Info{{Name: "test-1", Status: "running"}}, nil
	})

	// Populate cache (fresh)
	cache.Get()
	if callCount.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", callCount.Load())
	}

	// Call refreshAndGet directly — it should re-check TTL and serve from cache
	data, _, err := cache.refreshAndGet()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 1 {
		t.Errorf("expected 1 container from cache, got %d", len(data))
	}
	if callCount.Load() != 1 {
		t.Errorf("refreshAndGet should have re-checked TTL and NOT refreshed, got %d calls", callCount.Load())
	}
}

func TestToAPIContainers_AllFields(t *testing.T) {
	now := time.Now()
	input := []container.Info{
		{
			Name:          "test-1",
			ShortName:     "t-1",
			Status:        "running",
			StatusDetails: "Up 2 hours",
			Branch:        "main",
			AgentState:    "active",
			IsDormant:     true,
			AuthStatus:    "ok",
			LastActivity:  "2m ago",
			GitStatus:     "clean",
			CreatedAt:     now,
			CurrentTask:   "building",
			TaskProgress:  "3/5",
		},
	}

	result := toAPIContainers(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	r := result[0]
	if r.Name != "test-1" || r.ShortName != "t-1" || r.Status != "running" ||
		r.StatusDetails != "Up 2 hours" || r.Branch != "main" || r.AgentState != "active" ||
		!r.IsDormant || r.AuthStatus != "ok" || r.LastActivity != "2m ago" ||
		r.GitStatus != "clean" || !r.CreatedAt.Equal(now) ||
		r.CurrentTask != "building" || r.TaskProgress != "3/5" {
		t.Errorf("field mismatch in toAPIContainers conversion: %+v", r)
	}
}
