# Kata Containers Detection Package

This package provides runtime detection of Kata Containers on Kubernetes nodes, enabling NVSentinel components to automatically adapt their configuration based on the container runtime environment.

## Overview

Kata Containers use a different architecture than standard containers, running workloads in lightweight VMs for enhanced isolation. This affects how monitoring components access system resources like logs:

- **Standard Runtime**: Direct access to `/var/log` on the host
- **Kata Runtime**: Must access systemd journal via `/var/log/journal` and `/run/systemd/journal`

## Detection Methods

The detector uses multiple fallback methods for reliability:

### 1. Filesystem Detection (Primary)
- **Fastest**: No API calls required
- **Checks**:
  - Kata runtime binaries (`/opt/kata/bin/kata-runtime`, `/usr/bin/kata-runtime`)
  - Kata-specific directories (`/run/kata-containers`)
  - Cgroup hierarchy for kata references
  - Hypervisor indicators in `/proc/cpuinfo`

### 2. Kubernetes API Detection (Fallback)
- **Reliable**: Official node metadata
- **Checks**:
  - `node.Status.NodeInfo.ContainerRuntimeVersion` for "kata"
  - Node labels: `katacontainers.io/kata-runtime`, `kata-containers.io/runtime`, etc.
  - Node annotations: `kata-runtime.io/enabled`, `io.katacontainers.config`

## Usage

### Basic Usage

```go
import (
    "context"
    "github.com/nvidia/nvsentinel/health-monitors/syslog-health-monitor/pkg/kata"
    "k8s.io/client-go/kubernetes"
)

func main() {
    // Create detector with node name and optional Kubernetes client
    // Uses default 5-second timeout
    detector := kata.NewDetector(nodeName, clientset)
    
    // Detect Kata runtime
    isKata, err := detector.IsKataEnabled(context.Background())
    if err != nil {
        log.Fatalf("Detection failed: %v", err)
    }
    
    if isKata {
        // Configure for Kata environment
        logPath = "/nvsentinel/var/log/journal"
    } else {
        // Configure for standard runtime
        logPath = "/nvsentinel/var/log"
    }
}
```

### With Custom Timeout

For environments with slow filesystems or API responses, customize the detection timeout:

```go
import "time"

// Create detector with 10-second timeout instead of default 5 seconds
detector := kata.NewDetectorWithTimeout(nodeName, clientset, 10*time.Second)
isKata, err := detector.IsKataEnabled(context.Background())
```

### Without Kubernetes Client

For environments where API access is not available or desired:

```go
// Detector will only use filesystem-based detection
detector := kata.NewDetector(nodeName, nil)
isKata, err := detector.IsKataEnabled(context.Background())
```

## Scale Considerations

This package is designed for high-scale deployments (1000-2000+ nodes):

- **O(1) per node**: Each DaemonSet pod detects only its own node
- **No API bottlenecks**: Primary detection via filesystem (no API calls)
- **Fast execution**: Detection completes in milliseconds
- **Timeout protection**: 5-second timeout prevents hanging on slow filesystems
- **Fail-safe**: Returns `false` (standard runtime) if detection fails

## Detection Timeout

Detection operations have a configurable timeout to prevent hanging on slow filesystems or API calls:

```go
// Default timeout (5 seconds)
const DefaultDetectionTimeout = 5 * time.Second

// Use default timeout
detector := kata.NewDetector(nodeName, clientset)

// Use custom timeout for slower environments
detector := kata.NewDetectorWithTimeout(nodeName, clientset, 10*time.Second)
```

If detection exceeds the timeout, the operation returns false (assumes standard runtime).

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
- Filesystem detection with various cgroup configurations
- Kubernetes API detection with different node configurations
- Hypervisor detection in VM environments
- Error handling and edge cases
- Timeout behavior

## Integration with syslog-health-monitor

The detection is performed once at startup:

```go
func run() error {
    detector := kata.NewDetector(nodeName, nil)
    isKataRuntime, err := detector.IsKataEnabled(context.Background())
    if err != nil {
        slog.Warn("Kata detection failed, assuming standard runtime", "error", err)
        isKataRuntime = false
    }
    
    // Configure log paths based on detection result
    if isKataRuntime {
        // Mount journal paths
    } else {
        // Mount standard /var/log
    }
}
```

## Performance Characteristics

- **First-run latency**: < 100ms (filesystem check)
- **With API fallback**: < 500ms (single node GET request)
- **Memory overhead**: < 1KB per detector instance
- **CPU overhead**: Negligible (single detection at startup)

## Future Enhancements

Potential improvements for future versions:

1. **Caching**: Cache detection results in a ConfigMap for faster pod restarts
2. **Runtime Class Detection**: Check for RuntimeClass resources in the cluster
3. **Pod-level Detection**: Inspect specific pod runtimeClassName fields
4. **Metrics**: Export detection results as Prometheus metrics
5. **Dynamic Reconfiguration**: Watch for runtime changes and adapt without restart

## License

Copyright (c) 2025, NVIDIA CORPORATION. All rights reserved.
Licensed under the Apache License, Version 2.0.
