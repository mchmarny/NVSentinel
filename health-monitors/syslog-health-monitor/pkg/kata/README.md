# Kata Containers Detection Package

This package provides runtime detection of Kata Containers on Kubernetes nodes, enabling NVSentinel components to automatically adapt their configuration based on the container runtime environment.

## Overview

Kata Containers use a different architecture than standard containers, running workloads in lightweight VMs for enhanced isolation. This affects how monitoring components access system resources like logs:

- **Standard Runtime**: Direct access to `/var/log` on the host
- **Kata Runtime**: Must access systemd journal via `/var/log/journal` and `/run/systemd/journal`

## Security Requirements

### RBAC Permissions

The detector requires specific Kubernetes RBAC permissions when using API-based detection methods:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kata-detector
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list"]
- apiGroups: ["node.k8s.io"]
  resources: ["runtimeclasses"]
  verbs: ["get", "list"]
```

Apply this ClusterRole to your ServiceAccount:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kata-detector
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kata-detector
subjects:
- kind: ServiceAccount
  name: syslog-health-monitor
  namespace: nvsentinel
```

### Container Capabilities

Filesystem-based detection requires access to host `/proc` filesystem:

- **Volume Mounts**: Mount `/proc` from host to `/nvsentinel/proc` in container
- **Security Context**: No special capabilities required for read-only access
- **SELinux/AppArmor**: May require policy adjustments to read `/proc/1/cgroup`

**Important**: The detector uses **read-only** access and does not require `CAP_SYS_ADMIN` or privileged mode.

### Security Implications

1. **Node Information Disclosure**: API-based detection reads node metadata (labels, annotations, runtime version). Ensure RBAC policies align with your security model.

2. **Filesystem Access**: Detection reads host filesystem paths (`/proc`, `/opt`, `/usr/bin`). These are read-only operations but expose system configuration.

3. **Retry Mechanism**: The detector implements exponential backoff for API calls, which could amplify load during API server issues. Monitor `kata_detection_duration_seconds` metrics.

4. **RuntimeClass Enumeration**: Detection lists all RuntimeClasses in the cluster, which may expose cluster configuration to workloads.

## Detection Methods

The detector uses API-based detection with **intelligent caching for large-scale clusters (2000+ nodes)**:

### 1. Node Metadata Check (Primary)
- **Latency**: 50-200ms per detection
- **Caching**: Result cached by CachedDetector wrapper
- **Checks**:
  - `node.Status.NodeInfo.ContainerRuntimeVersion` for "kata"
  - Node labels: `katacontainers.io/kata-runtime`, `kata-containers.io/runtime`, etc.
  - Node annotations: `kata-runtime.io/enabled`, `io.katacontainers.config`

### 2. RuntimeClass Detection (Secondary)
- **Latency**: <1ms (cached), 50-200ms (uncached)
- **Caching**: Automatic TTL cache (default 10 minutes)
- **Scalability**: Reduces API calls by 98% in large clusters
- **Checks**: RuntimeClass resources with kata-related handlers

### Architecture Note

**Filesystem detection was removed** due to false positives. Node filesystem artifacts (binaries, directories) persist after Kata is disabled, making filesystem state unreliable. Only Kubernetes API-based detection reflects the current, active runtime configuration.

Both methods run **concurrently** using errgroup for fast detection, with early exit on first positive result.

## Scaling to Large Clusters

### RuntimeClass Caching (Automatic)
**Reduces API load by 98%** with zero configuration:
- Default **10-minute TTL** cache for RuntimeClass List() results
- Configurable via `WithRuntimeClassCacheTTL(duration)`
- 2000 pods with 10-minute cache = ~3 RuntimeClass Lists/min (vs 133/min without)

### API Server Load Comparison

| Configuration | Node Gets/detection | RuntimeClass Lists/detection | Effective Load |
|--------------|---------------------|------------------------------|----------------|
| **No caching** | 1 | 1 | High (every detection) |
| **With cache (default)** | 1 | <0.01 (10min TTL) | Low |
| **With CachedDetector** | <0.01 (cache) | <0.01 (cache) | Minimal |

*With CachedDetector (15min TTL) + RuntimeClass cache (10min TTL): 2000 pods = ~3 API calls/min*

### Monitoring

Use Prometheus metrics to validate performance:

```promql
# Detection latency
histogram_quantile(0.99, 
  rate(kata_detection_duration_seconds_bucket[5m]))

# Detection attempts
sum(rate(kata_detection_attempts_total[5m])) by (method)

# Cache effectiveness (RuntimeClass)
rate(kata_detection_duration_seconds{method="runtime-class"}[5m])
```

## Usage

### Recommended Usage (Production)

```go
import (
    "context"
    "time"
    "github.com/nvidia/nvsentinel/health-monitors/syslog-health-monitor/pkg/kata"
    "k8s.io/client-go/kubernetes"
)

func main() {
    // Create detector with optional custom cache TTL
    detector, err := kata.NewDetector(nodeName, clientset,
        kata.WithMetrics(true),                        // Enable Prometheus metrics
        kata.WithRuntimeClassCacheTTL(10*time.Minute), // Default, can adjust
        kata.WithTimeout(5*time.Second))               // Detection timeout
    if err != nil {
        log.Fatalf("Failed to create detector: %v", err)
    }
    
    // Wrap with CachedDetector for repeated checks
    cached := kata.NewCachedDetector(detector, 15*time.Minute)
    
    // Detection uses cached results when available
    result, err := cached.IsKataEnabled(context.Background())
    if err != nil {
        log.Fatalf("Detection failed: %v", err)
    }
    
    if result.IsKata {
        log.Printf("Kata detected via %s", result.Method)
    }
}
```

### Simple Usage (One-time Detection)

```go
import (
    "context"
    "github.com/nvidia/nvsentinel/health-monitors/syslog-health-monitor/pkg/kata"
    "k8s.io/client-go/kubernetes"
)

func main() {
    // Simple detector - caching still active internally
    detector, err := kata.NewDetector(nodeName, clientset)
    if err != nil {
        log.Fatalf("Failed to create detector: %v", err)
    }
    
    // Detect Kata runtime - returns detailed result
    result, err := detector.IsKataEnabled(context.Background())
    if err != nil {
        log.Fatalf("Detection failed: %v", err)
    }
    
    if result.IsKata {
        log.Printf("Kata detected via %s", result.Method)
    }
}
```

**When to skip informers:**
- ⚠️ Short-lived CLI tools or one-off scripts
- ⚠️ Unit tests (use fake clientset instead)
- ⚠️ Environments where `watch` RBAC verb is restricted

### Advanced: Force Direct API Calls

```go
// Explicitly disable informers even if initialized (rare use case)
detector, err := kata.NewDetector(nodeName, clientset,
    kata.WithInformers(false),  // Force direct API calls
    kata.WithMetrics(true))
```

**Use cases:**
- Comparing performance metrics between informer vs direct API
- Debugging informer cache issues
- Testing API server load under different configurations

## Configuration Options

### Functional Options Pattern
    detector, err := kata.NewDetector(nodeName, clientset,
        kata.WithInformers(true),              // Use informer cache (recommended)
        kata.WithRuntimeClassCacheTTL(30*time.Minute), // Longer cache for stable clusters
        kata.WithMetrics(true),                // Enable Prometheus metrics
        kata.WithTimeout(3*time.Second))       // Shorter timeout with informers
    if err != nil {
    if err != nil {
        log.Fatalf("Failed to create detector: %v", err)
    }
    
    // Direct detection - no outer cache
    result, err := detector.IsKataEnabled(context.Background())
    if err != nil {
        log.Fatalf("Detection failed: %v", err)
    }
    
    if result.IsKata {
        log.Printf("Kata detected via %s method", result.Method)
        // Configure for Kata environment
        logPath = "/nvsentinel/var/log/journal"
    } else {
        log.Printf("Standard runtime detected (attempted: %v)", result.AttemptedMethods)
        // Configure for standard runtime
        logPath = "/nvsentinel/var/log"
    }
    
    // Check for detection errors (non-fatal)
    if len(result.Errors) > 0 {
        log.Printf("Detection encountered %d errors: %v", len(result.Errors), result.Errors)
    }
}
```

## Configuration Options
if err != nil {
    log.Fatalf("Failed to create detector: %v", err)
}

// Wrap with cache (5 minute TTL)
cachedDetector := kata.NewCachedDetector(detector, 5*time.Minute)

// First call performs detection
result1, err := cachedDetector.IsKataEnabled(context.Background())

// Subsequent calls within TTL return cached result (no I/O)
result2, err := cachedDetector.IsKataEnabled(context.Background())

// Invalidate cache if needed (e.g., after configuration change)
cachedDetector.InvalidateCache()
```

### Without Kubernetes Client

For environments where API access is not available or desired:

```go
// Detector will only use filesystem-based detection
detector, err := kata.NewDetector(nodeName, nil)
if err != nil {
    log.Fatalf("Invalid node name: %v", err)
}

result, err := detector.IsKataEnabled(context.Background())
```

### Handling Detection Results

The new `DetectionResult` type provides comprehensive information:

```go
result, err := detector.IsKataEnabled(ctx)
if err != nil {
    // Fatal errors (timeout, context cancellation)
    log.Fatalf("Detection failed: %v", err)
}

// Check what was detected
if result.IsKata {
    fmt.Printf("Kata runtime detected via %s\n", result.Method)
} else {
    fmt.Printf("Standard runtime (tried %d methods)\n", len(result.AttemptedMethods))
}

// Examine non-fatal errors
for i, err := range result.Errors {
    log.Printf("Detection method %d encountered error: %v", i+1, err)
}

// Log attempted methods for debugging
log.Printf("Attempted detection methods: %v", result.AttemptedMethods)
```

## Scale Considerations

This package is **production-ready for clusters with 2000-5000+ nodes**:

### Per-Node Architecture
- **O(1) per node**: Each DaemonSet pod detects only its own node
- **No cross-node queries**: Never lists all 2000 nodes
- **Independent operation**: Pod failures don't affect other nodes

### Optimization Layers
1. **RuntimeClass Cache** (automatic):
   - 30-minute TTL reduces List() calls by 98%
   - 2000 pods = ~8 API calls/min vs 400 without cache
   
2. **Informers** (opt-in via `WithInformers(true)`):
   - Single Watch per resource type (not per pod)
   - Local in-memory cache with automatic updates
   - Zero API server load after initial sync
   - **Recommended for clusters > 500 nodes**

3. **Detection Flow** (automatic):
   - Single node Get() shared by both methods
   - Concurrent execution with errgroup
   - Early exit on first positive result

### Performance Characteristics

| Configuration | Latency | API Calls/Detection | Memory/Pod |
|--------------|---------|---------------------|------------|
| Default (cached RuntimeClass) | 50-200ms | 1 Get + 1 List (cached) | ~2KB |
| With Informers | 1-10ms | 0 (cache hit) | ~2KB + shared informer |
| With CachedDetector | 0ms (cache hit) | 0 | ~2KB |

### Scalability Validation

Monitor these metrics in production:

```promql
# P99 latency should be <10ms with informers, <200ms without
histogram_quantile(0.99, 
  rate(kata_detection_duration_seconds_bucket[5m]))

# API call rate should approach 0 with informers enabled
sum(rate(kata_detection_attempts_total{method="kubernetes-api"}[5m]))

# Cache effectiveness: higher = better (seconds since last RuntimeClass List)
time() - kata_detection_duration_seconds{method="runtime-class"}
```

### Deployment Recommendations

| Cluster Size | Configuration | Expected Load |
|-------------|--------------|---------------|
| < 500 nodes | Default (no informers) | ~100 API calls/min |
| 500-2000 nodes | Enable informers | ~0 API calls/min |
| 2000-5000 nodes | Informers + longer cache TTL | ~0 API calls/min |
| 5000+ nodes | Informers required | ~0 API calls/min |
- **Cache overhead**: 16 bytes per cached result

## Observability

### Prometheus Metrics

The detector exports the following metrics (when `WithMetrics(true)` is enabled):

```promql
# Detection duration histogram by method and result
kata_detection_duration_seconds{node="node-1", method="filesystem|kubernetes-api|runtime-class", result="true|false"}

# Detection attempts counter by method and success
kata_detection_attempts_total{node="node-1", method="filesystem|kubernetes-api|runtime-class", success="true|false"}

# Overall detection results counter
kata_detection_results_total{node="node-1", detected="true|false|timeout"}
```

### Example Queries

```promql
# P99 detection latency across all nodes
histogram_quantile(0.99, sum(rate(kata_detection_duration_seconds_bucket[5m])) by (le))

# Detection failure rate by method
rate(kata_detection_attempts_total{success="false"}[5m]) / rate(kata_detection_attempts_total[5m])

# Nodes with Kata runtime enabled
count(kata_detection_results_total{detected="true"})
```

## Failure Modes and Operational Impact

### 1. All Detection Methods Fail
**Behavior**: Returns `DetectionResult{IsKata: false, Errors: [...]}`
**Impact**: System assumes standard runtime, may fail to access logs in Kata environment
**Mitigation**: 
- Monitor `result.Errors` and alert on consistent failures
- Check RBAC permissions and filesystem mounts
- Review `kata_detection_attempts_total{success="false"}` metric

### 2. API Server Unavailable
**Behavior**: Filesystem detection succeeds, API methods fail with retry
**Impact**: Minimal - detection completes via filesystem in < 10ms
**Mitigation**: API failures are logged but not fatal

### 3. Timeout Exceeded
**Behavior**: Returns context deadline error
**Impact**: Detection fails, application may not start
**Mitigation**: 
- Increase timeout via `WithTimeout()` option
- Investigate slow filesystem or API performance
- Monitor `kata_detection_duration_seconds` for outliers

### 4. Invalid Node Name
**Behavior**: `NewDetector()` returns validation error immediately
**Impact**: Application fails to start with clear error message
**Mitigation**: Ensure `NODE_NAME` environment variable uses DNS-1123 format

### 5. RBAC Permissions Missing
**Behavior**: API and RuntimeClass methods fail, filesystem may succeed
**Impact**: Reduced detection reliability in API-only scenarios
**Mitigation**: Apply RBAC ClusterRole (see Security Requirements section)

### 6. Cgroup v2 Not Supported (Old Kernel)
**Behavior**: Automatically falls back to cgroup v1 detection
**Impact**: None - transparent fallback
**Mitigation**: No action needed, fallback is automatic

## Detection Timeout

Detection operations have a configurable timeout to prevent hanging on slow filesystems or API calls:

```go
// Default timeout (5 seconds)
const DefaultDetectionTimeout = 5 * time.Second

// Use default timeout
detector, _ := kata.NewDetector(nodeName, clientset)

// Use custom timeout for slower environments
detector, _ := kata.NewDetector(nodeName, clientset, kata.WithTimeout(10*time.Second))
```

If detection exceeds the timeout, the operation returns a context deadline error.

**Timeout Guidelines:**
- **Fast clusters**: 3-5 seconds (default: 5s)
- **Standard clusters**: 5-10 seconds
- **Slow/large clusters**: 10-15 seconds
- **Maximum recommended**: 30 seconds

## Testing

The package includes comprehensive unit tests:

```bash
go test ./pkg/kata/...
```

Test coverage includes:
- Filesystem detection with various cgroup configurations (v1 and v2)
- Kubernetes API detection with different node configurations
- RuntimeClass detection with kata handlers
- Hypervisor detection in VM environments
- Error handling and edge cases
- Timeout behavior
- Concurrent detection with errgroup
- Result caching with TTL expiration
- Input validation for node names

Run with coverage:
```bash
go test ./pkg/kata/... -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Integration with syslog-health-monitor

The detection is performed once at startup:

```go
func run() error {
    // Create detector with error handling
    detector, err := kata.NewDetector(nodeName, nil)
    if err != nil {
        return fmt.Errorf("failed to create kata detector: %w", err)
    }
    
    // Perform detection with context
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    result, err := detector.IsKataEnabled(ctx)
    if err != nil {
        // Fatal error - cannot proceed
        return fmt.Errorf("kata detection failed: %w", err)
    }
    
    // Log detection result
    if result.IsKata {
        slog.Info("Kata runtime detected", 
            "method", result.Method,
            "node", nodeName)
    } else {
        slog.Info("Standard runtime detected",
            "attempted_methods", result.AttemptedMethods,
            "node", nodeName)
    }
    
    // Log non-fatal errors for debugging
    for _, detectionErr := range result.Errors {
        slog.Warn("Detection method encountered error", "error", detectionErr)
    }
    
    // Configure log paths based on detection result
    if result.IsKata {
        // Mount journal paths for Kata
        logPaths = []string{
            "/nvsentinel/var/log/journal",
            "/nvsentinel/run/systemd/journal",
        }
    } else {
        // Mount standard /var/log for regular containers
        logPaths = []string{
            "/nvsentinel/var/log",
        }
    }
    
    // Continue with monitor initialization
    return startMonitor(logPaths)
}
```

## Migration from Old API

The API has changed to return `DetectionResult` instead of `bool`. Here's how to migrate:

### Old Code (v1)
```go
detector := kata.NewDetector(nodeName, clientset)
isKata, err := detector.IsKataEnabled(ctx)
if err != nil {
    log.Fatalf("Detection failed: %v", err)
}
if isKata {
    // configure for kata
}
```

### New Code (v2)
```go
detector, err := kata.NewDetector(nodeName, clientset)
if err != nil {
    log.Fatalf("Invalid configuration: %v", err)
}

result, err := detector.IsKataEnabled(ctx)
if err != nil {
    log.Fatalf("Detection failed: %v", err)
}

if result.IsKata {
    // configure for kata
    log.Printf("Detected via: %s", result.Method)
}

// Optionally log detection errors
if len(result.Errors) > 0 {
    log.Printf("Detection warnings: %v", result.Errors)
}
```

**Breaking Changes:**
1. `NewDetector()` now returns `(*Detector, error)` instead of `*Detector`
2. `IsKataEnabled()` returns `(*DetectionResult, error)` instead of `(bool, error)`
3. `NewDetectorWithTimeout()` is deprecated (use `WithTimeout` option instead)

## Performance Characteristics

- **First-run latency**: < 100ms (filesystem check)
- **With API fallback**: < 500ms (single node GET request)
- **Memory overhead**: < 1KB per detector instance
- **CPU overhead**: Negligible (single detection at startup)

## Future Enhancements

Potential improvements for future versions:

1. **~~Caching~~**: ✅ Implemented via `CachedDetector`
2. **~~Runtime Class Detection~~**: ✅ Implemented via `detectViaRuntimeClass`
3. **Pod-level Detection**: Inspect specific pod runtimeClassName fields
4. **~~Metrics~~**: ✅ Prometheus metrics exported
5. **Dynamic Reconfiguration**: Watch for runtime changes and adapt without restart
6. **Webhook Integration**: Expose detection as admission webhook for pod scheduling

## License

Copyright (c) 2025, NVIDIA CORPORATION. All rights reserved.
Licensed under the Apache License, Version 2.0.
