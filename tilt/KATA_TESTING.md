# Testing Kata Detection in Tilt

This guide explains how to test Kata Containers detection with mock nodes in your Tilt development environment.

## Overview

The Tilt setup creates two types of KWOK (fake) nodes:
- **Regular nodes** (75% of total): Standard container runtime, kata detection returns `false`
- **Kata nodes** (25% of total): Kata-enabled runtime, kata detection returns `true`

## Node Distribution

With the default `NUM_GPU_NODES=50`:
- **37 regular nodes**: `kwok-node-0` through `kwok-node-36`
- **13 Kata nodes**: `kwok-kata-node-0` through `kwok-kata-node-12`

## Kata Detection Methods

The mock Kata nodes have all detection signals configured:

1. **RuntimeVersion Detection** (Primary):
   ```yaml
   containerRuntimeVersion: containerd://1.7.0-kata
   ```

2. **Node Annotation Detection**:
   ```yaml
   io.katacontainers.config.runtime.oci_runtime: kata-runtime
   ```

3. **Node Label Detection**:
   ```yaml
   katacontainers.io/kata-runtime: "true"
   ```

4. **RuntimeClass Detection**:
   - `kata-qemu`, `kata-clh`, `kata-fc` RuntimeClasses are created

## How It Works

1. **KWOK nodes are created** with appropriate runtime signatures
2. **Labeler-module detects Kata** on each node using the kata detector
3. **Labeler sets the label**: `nvsentinel.dgxc.nvidia.com/kata.enabled: "true"` or `"false"`
4. **DaemonSets schedule accordingly**:
   - `syslog-health-monitor-kata` → Kata nodes (with systemd journal mounts)
   - `syslog-health-monitor-regular` → Regular nodes (with /var/log mounts)

## Testing in Tilt

### Start Tilt
```bash
cd tilt
tilt up
```

### Verify Node Labels

Check that the labeler has set kata labels on nodes:

```bash
# Check Kata nodes - should have kata.enabled: "true"
kubectl get nodes -l type=kwok,nvsentinel.dgxc.nvidia.com/kata.enabled=true -o custom-columns=NAME:.metadata.name,KATA:.metadata.labels.nvsentinel\.dgxc\.nvidia\.com/kata\.enabled

# Check regular nodes - should have kata.enabled: "false"
kubectl get nodes -l type=kwok,nvsentinel.dgxc.nvidia.com/kata.enabled=false -o custom-columns=NAME:.metadata.name,KATA:.metadata.labels.nvsentinel\.dgxc\.nvidia\.com/kata\.enabled

# List all nodes with their kata status
kubectl get nodes -l type=kwok -o custom-columns=NAME:.metadata.name,KATA:.metadata.labels.nvsentinel\.dgxc\.nvidia\.com/kata\.enabled
```

### Verify DaemonSet Scheduling

Check that the correct DaemonSet pods are scheduled on the correct nodes:

```bash
# Kata DaemonSet should be on kata nodes
kubectl get pods -n nvsentinel -l nvsentinel.dgxc.nvidia.com/kata=true -o wide

# Regular DaemonSet should be on regular nodes
kubectl get pods -n nvsentinel -l nvsentinel.dgxc.nvidia.com/kata=false -o wide
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

## Customizing Node Distribution

You can adjust the number and ratio of nodes:

### Change Total Node Count
```bash
NUM_GPU_NODES=100 tilt up
```
This creates 75 regular nodes + 25 Kata nodes.

### Modify the Ratio

Edit `tilt/Tiltfile` and change this line:
```python
num_regular_nodes = int(num_gpu_nodes * 0.75)  # Change 0.75 to desired ratio
```

For example, `0.5` would create 50/50 split.

## Files

- **`kwok-node-template.yaml`**: Template for regular nodes
- **`kwok-kata-node-template.yaml`**: Template for Kata-enabled nodes  
- **`kata-runtimeclass.yaml`**: RuntimeClass resources for Kata detection
- **`Tiltfile`**: Creates nodes with configurable ratio

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

Delete and recreate a node to trigger relabeling (pick any node):
```bash
# Example: delete a Kata node
kubectl get nodes -l type=kwok,nvsentinel.dgxc.nvidia.com/kata.enabled=true -o name | head -1 | xargs kubectl delete
# Tilt will recreate it
```

## What Gets Tested

With this setup, you can test:

1. ✅ **Kata detection** via multiple methods (runtime version, labels, annotations, RuntimeClass)
2. ✅ **Automatic node labeling** by the labeler-module
3. ✅ **DaemonSet scheduling** to correct node types
4. ✅ **Different volume mounts** (systemd journal vs /var/log)
5. ✅ **Scale testing** with many nodes (adjust `NUM_GPU_NODES`)

## Next Steps

Once verified in Tilt, the same pattern will work in real clusters where:
- Some nodes have Kata Containers installed
- The labeler will detect actual Kata presence
- DaemonSets will schedule with the correct configurations
