// Copyright (c) 2025, NVIDIA CORPORATION.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kata

import (
	"context"
	"sync"
	"time"
)

// CachedDetector wraps a Detector with caching to avoid redundant detection
// operations. Reduces latency on pod restarts when runtime config is unchanged.
type CachedDetector struct {
	detector     *Detector
	mu           sync.RWMutex
	cachedResult *DetectionResult
	cachedAt     time.Time
	cacheTTL     time.Duration
}

// NewCachedDetector creates a cached detector wrapper with the specified TTL.
// Typical production TTL is 5-15 minutes since runtime config changes rarely.
// Returns nil if detector is nil.
func NewCachedDetector(detector *Detector, ttl time.Duration) *CachedDetector {
	if detector == nil {
		return nil
	}

	return &CachedDetector{
		detector: detector,
		cacheTTL: ttl,
	}
}

// IsKataEnabled returns cached result if available and fresh, otherwise
// performs new detection and caches it. Safe for concurrent use.
func (cd *CachedDetector) IsKataEnabled(ctx context.Context) (*DetectionResult, error) {
	// Fast path: check cache with read lock
	cd.mu.RLock()
	if cd.cachedResult != nil && time.Since(cd.cachedAt) < cd.cacheTTL {
		result := cd.cachedResult
		cd.mu.RUnlock()

		return result, nil
	}
	cd.mu.RUnlock()

	// Cache miss or expired: acquire write lock
	cd.mu.Lock()
	defer cd.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have updated)
	if cd.cachedResult != nil && time.Since(cd.cachedAt) < cd.cacheTTL {
		return cd.cachedResult, nil
	}

	// Perform detection
	result, err := cd.detector.IsKataEnabled(ctx)
	if err == nil {
		cd.cachedResult = result
		cd.cachedAt = time.Now()
	}

	return result, err
}

// InvalidateCache clears cached result, forcing fresh detection on next call.
// Use after config changes or when cached result may be stale.
func (cd *CachedDetector) InvalidateCache() {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	cd.cachedResult = nil
	cd.cachedAt = time.Time{}
}
