# Testing Kata Detection in Tilt

This guide explains how to test Kata Containers detection with mock nodes in your Tilt development environment.

## Overview

The Tilt setup creates two types of KWOK (fake) nodes:
- **Regular GPU nodes** (`NUM_GPU_NODES`): Standard container runtime, kata detection returns `false`
- **Kata test nodes** (`NUM_KATA_TEST_NODES`): Separate kata-enabled nodes for testing

This approach ensures kata testing doesn't interfere with other tests that expect all GPU nodes to be regular nodes.

## Node Distribution

With the default environment variables:
- **50 regular GPU nodes**: `kwok-node-0` through `kwok-node-49` (controlled by `NUM_GPU_NODES=50`)
- **5 kata test nodes**: `kwok-kata-test-node-0` through `kwok-kata-test-node-4` (controlled by `NUM_KATA_TEST_NODES=5`)

Total: 55 nodes (50 regular + 5 kata test nodes)

## Kata Detection Methods

The labeler-module uses **label-based detection** to identify Kata-enabled nodes.

### Detection Process

1. **Input Labels Checked** (for detection):
   - Default: `katacontainers.io/kata-runtime` (must have truthy value)
   - Optional: Custom label via `--kata-label` flag (if configured)
   - Truthy values: `"true"`, `"enabled"`, `"1"`, `"yes"` (case-insensitive)

2. **Output Label Set** (detection result):
   - `nvsentinel.dgxc.nvidia.com/kata.enabled: "true"` (if Kata detected)
   - `nvsentinel.dgxc.nvidia.com/kata.enabled: "false"` (if not detected)

### Mock Node Configuration

The kata test nodes in `kwok-kata-test-node-template.yaml` are configured with:
- **Node Label**: `katacontainers.io/kata-runtime: "true"` (used for detection)
- **Node Annotation**: `io.katacontainers.config.runtime.oci_runtime: kata-runtime` (for reference only)

**Note**: While the mock nodes include annotations and runtime version metadata for realism,
the labeler-module currently only checks node labels for Kata detection.

## How It Works

1. **KWOK nodes are created** with kata labels already set
2. **Labeler-module checks input labels** (`katacontainers.io/kata-runtime` and optional custom labels)
3. **Labeler sets the output label**: `nvsentinel.dgxc.nvidia.com/kata.enabled: "true"` or `"false"`
4. **DaemonSets schedule accordingly**:
   - `syslog-health-monitor-kata` → Kata nodes (with systemd journal mounts)
   - `syslog-health-monitor-regular` → Regular nodes (with /var/log mounts)

## Environment Variables

Control the number of nodes via environment variables:

```bash
# Set number of regular GPU nodes (default: 50)
export NUM_GPU_NODES=50

# Set number of kata test nodes (default: 5, set to 0 to disable kata testing)
export NUM_KATA_TEST_NODES=5
```

## Testing in Tilt

### Start Tilt
```bash
cd tilt
tilt up
```

### Verify Node Labels

Check that the labeler has set kata labels on nodes:

```bash
# Check kata test nodes - should have kata.enabled: "true"
kubectl get nodes -l type=kwok,nvsentinel.dgxc.nvidia.com/kata.enabled=true -o custom-columns=NAME:.metadata.name,KATA:.metadata.labels.nvsentinel\.dgxc\.nvidia\.com/kata\.enabled

# Check regular GPU nodes - should have kata.enabled: "false"
kubectl get nodes -l type=kwok,nvsentinel.dgxc.nvidia.com/kata.enabled=false -o custom-columns=NAME:.metadata.name,KATA:.metadata.labels.nvsentinel\.dgxc\.nvidia\.com/kata\.enabled

# List all KWOK nodes with their kata status
kubectl get nodes -l type=kwok -o custom-columns=NAME:.metadata.name,KATA:.metadata.labels.nvsentinel\.dgxc\.nvidia\.com/kata\.enabled
```

### Verify DaemonSet Scheduling

```bash
# Check kata daemonset - should run on kata test nodes (5 pods by default)
kubectl get pods -n nvsentinel -l app.kubernetes.io/name=syslog-health-monitor-kata -o wide

# Check regular daemonset - should run on regular GPU nodes (50 pods by default)
kubectl get pods -n nvsentinel -l app.kubernetes.io/name=syslog-health-monitor-regular -o wide
```

### Check Labeler Logs

Watch the labeler detect Kata on nodes:

```bash
kubectl logs -n nvsentinel -l app.kubernetes.io/name=labeler -f | grep -i kata
```

You should see messages like:
```
INFO Setting Kata enabled label on node node=kwok-kata-node-0 kata=true
INFO Setting Kata enabled label on node node=kwok-node-0 kata=false
```

## Adjusting Node Counts

Change node counts via environment variables before starting Tilt:

```bash
# More GPU nodes for scale testing
export NUM_GPU_NODES=100

# More kata test nodes for kata-specific testing
export NUM_KATA_TEST_NODES=10

# Disable kata testing entirely (useful for tests that don't need kata)
export NUM_KATA_TEST_NODES=0

tilt up
```

## Files

- **`kwok-node-template.yaml`**: Template for regular GPU nodes
- **`kwok-kata-node-template.yaml`**: Template for kata test nodes  
- **`Tiltfile`**: Creates nodes with configurable counts via environment variables

## Troubleshooting

### Nodes Not Getting Labels

1. Check labeler is running:
   ```bash
   kubectl get pods -n nvsentinel -l app.kubernetes.io/name=labeler
   ```

2. Check for errors:
   ```bash
   kubectl logs -n nvsentinel -l app.kubernetes.io/name=labeler
   ```

3. Verify nodes have driver pods (labeler triggers on pod events):
   ```bash
   kubectl get pods -n gpu-operator -o wide
   ```

### DaemonSets Not Scheduling

1. Check if labels are set on any Kata node:
   ```bash
   # Pick any Kata node to inspect
   kubectl get nodes -l type=kwok,nvsentinel.dgxc.nvidia.com/kata.enabled=true -o name | head -1 | xargs kubectl describe | grep kata.enabled
   ```

2. Check DaemonSet selectors match:
   ```bash
   kubectl get daemonset -n nvsentinel syslog-health-monitor-kata -o yaml | grep -A5 nodeSelector
   ```

### Force Relabeling

Delete and recreate a node to trigger relabeling:
```bash
# Example: delete a kata test node
kubectl delete node kwok-kata-test-node-0
# Tilt will recreate it

# Example: delete a regular GPU node
kubectl delete node kwok-node-0
# Tilt will recreate it
```

## What Gets Tested

With this setup, you can test:

1. ✅ **Kata detection** via node metadata (runtime version, labels, annotations)
2. ✅ **Automatic node labeling** by the labeler-module
3. ✅ **DaemonSet scheduling** to correct node types
4. ✅ **Different volume mounts** (systemd journal vs /var/log)
5. ✅ **Scale testing** with many nodes (adjust `NUM_GPU_NODES`)

## Next Steps

Once verified in Tilt, the same pattern will work in real clusters where:
- Some nodes have Kata Containers installed
- The labeler will detect actual Kata presence
- DaemonSets will schedule with the correct configurations
