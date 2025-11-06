# NVSentinel Integration

> Note: code implementation links are for document review purposes only, will be removed before publishing

## Overview

NVSentinel exposes GPU and hardware health status through native Kubernetes primitives: **Node Conditions**, **Taints**, and **Labels**. This document defines the standardized conventions that external systems can rely on for integration, monitoring, and scheduling decisions.

- Platform teams can monitor node health using standard Kubernetes tools
- Schedulers can make informed placement decisions based on hardware state
- Monitoring systems can alert on specific hardware failures
- External automation can respond to health events without NVSentinel-specific knowledge

## Architecture

NVSentinel uses a three-layer approach to signal node health:

1. **Node Conditions** (Layer 1) - Set by Platform Connectors based on health events
   - **Function:** `updateNodeConditions()` in `platform-connectors/pkg/connectors/kubernetes/process_node_events.go:44`
   - **Updates:** `node.Status.Conditions` via `clientset.CoreV1().Nodes().UpdateStatus()`
   
2. **Taints** (Layer 2) - Applied by Fault Quarantine based on CEL rules
   - **Function:** `QuarantineNodeAndSetAnnotations()` → `applyTaints()` in `fault-quarantine/pkg/informer/k8s_client.go:197-233`
   - **Updates:** `node.Spec.Taints` via `clientset.CoreV1().Nodes().Update()`
   
3. **Labels** (Layer 3) - Maintained by Labeler and Platform Connectors for metadata
   - **Functions:** 
     - `updateNodeLabelsForPod()` in `labeler/pkg/labeler/labeler.go:394` (DCGM/driver labels)
     - `updateKataLabel()` in `labeler/pkg/labeler/labeler.go:468` (Kata labels)
   - **Updates:** `node.Labels` via `clientset.CoreV1().Nodes().Update()`

```
┌─────────────────────┐
│  Health Monitors    │ GPU, Syslog, CSP health detection
└──────────┬──────────┘
           │ gRPC HealthEvent
           ▼
┌─────────────────────┐
│ Platform Connectors │ Validates and persists events
└──────────┬──────────┘
           │
           ├─────────────► MongoDB (event store)
           │
           └─────────────► Kubernetes API: Set NodeCondition
                                            (checkName → ConditionType)

MongoDB Change Stream
           │
           ▼
┌─────────────────────┐
│ Fault Quarantine    │ Evaluates CEL rules
└──────────┬──────────┘
           │
           └─────────────► Kubernetes API: Apply Taint + Cordon
                                            (based on ruleset config)

┌─────────────────────┐
│ Labeler             │ Watches pods and nodes
└──────────┬──────────┘
           │
           └─────────────► Kubernetes API: Set Labels
                                            (DCGM version, driver status, kata)
```

## Node Conditions

### Overview

Platform Connectors set NodeConditions directly based on the `checkName` field from HealthEvents. The condition type is the check name itself, creating a 1:1 mapping.

**Code Reference:** `platform-connectors/pkg/connectors/kubernetes/process_node_events.go:83,328`
```go
conditionType := corev1.NodeConditionType(string(event.CheckName))
```

### Naming Convention

- **Format:** PascalCase, descriptive names from health monitor check names
- **Examples:** `GpuMemoryError`, `NVLinkDown`, `ThermalThrottle`, `CSPMaintenance`

### Condition Status Semantics

| Status | Meaning | When Set |
|--------|---------|----------|
| `True` | Error/fault detected | HealthEvent with `isFatal=true` or `isHealthy=false` |
| `False` | Component healthy | HealthEvent with `isHealthy=true` |
| `Unknown` | Health state cannot be determined | Initial state or monitoring failure |

### Condition Message Format

Condition messages follow this pattern:
```
[ErrorCode1, ErrorCode2] Human-readable description - RecommendedAction: ACTION_NAME
```

**Example:**
```yaml
conditions:
  - type: GpuMemoryError
    status: "True"
    reason: HardwareFailure
    message: "[DCGM_FR_FAULTY_MEMORY] GPU memory failure detected - RecommendedAction: RESTART_VM"
    lastTransitionTime: "2025-11-06T10:00:00Z"
```

### Standard Condition Types by Component

#### GPU Conditions
These conditions are set by `gpu-health-monitor` based on DCGM diagnostics:

- `GpuMemoryError` - GPU memory failures (ECC errors, faulty memory)
- `GpuThermalWatch` - Thermal throttling or temperature violations
- `GpuPcieWatch` - PCIe link issues (replay rate, bandwidth)
- `GpuXidError` - GPU XID critical errors
- `GpuPowerWatch` - Power-related issues
- `GpuInforomCorrupt` - Inforom corruption detected

#### NVLink Conditions
- `NVLinkDown` - NVLink connection down
- `NVLinkCrcError` - NVLink CRC error threshold exceeded
- `NVLinkErrorCritical` - Critical NVLink errors

#### NVSwitch Conditions
- `NVSwitchFatalError` - Fatal NVSwitch hardware error
- `NVSwitchDown` - NVSwitch unavailable
- `NVSwitchNonFatalError` - Non-fatal NVSwitch errors (warnings)

#### System Conditions
- `DCGMError` - DCGM daemon or API failures
- `DriverError` - NVIDIA driver issues
- `CSPMaintenance` - Cloud provider scheduled maintenance (set by `csp-health-monitor`)
- `SyslogError` - System log analysis detected issues (set by `syslog-health-monitor`)

### Implementation Details

**Platform Connectors Module** sets conditions as follows:

1. Receives HealthEvent via gRPC
2. Extracts `checkName` field as the condition type
3. Determines status based on `isFatal` and `isHealthy` flags
4. Constructs message from `errorCode`, `message`, and `recommendedAction`
5. Updates node via Kubernetes API with retry logic

**Code Location:** `platform-connectors/pkg/connectors/kubernetes/process_node_events.go`

## Taints

### Overview

Taints are applied by the Fault Quarantine module based on configurable CEL rules. Unlike conditions (which are always set), taints are optional and configured per deployment based on operational policies.

### Naming Convention

**Format:** `component.health/error-type`

| Prefix | Component | Example Keys |
|--------|-----------|--------------|
| `gpu.health/` | GPU-specific errors | `gpu.health/memory-error`, `gpu.health/thermal-warning` |
| `nvlink.health/` | NVLink errors | `nvlink.health/link-down`, `nvlink.health/crc-error` |
| `nvswitch.health/` | NVSwitch errors | `nvswitch.health/fatal-error` |
| `system.health/` | Driver, DCGM, CSP | `system.health/driver-error`, `system.health/dcgm-failure` |

### Taint Values

Taint values indicate severity:

- **`fatal`** - Requires immediate remediation, node is unsafe for workloads
- **`degraded`** - Performance impact, workloads may run with reduced capability
- **`warning`** - Monitor only, workloads can continue normally

### Taint Effect Guidelines

| Effect | Use Case | Impact |
|--------|----------|--------|
| `NoSchedule` | Fatal errors requiring remediation | New pods without toleration won't be scheduled |
| `PreferNoSchedule` | Degraded state or warnings | Scheduler tries to avoid but will schedule if necessary |
| `NoExecute` | Immediate evacuation needed | Existing pods without toleration are evicted (rarely used) |

### Configuration

Taints are defined in Fault Quarantine rulesets using CEL expressions:

```toml
[[rule-sets]]
  name = "Fatal GPU Memory Error"
  priority = 100
  
  [[rule-sets.match.any]]
    kind = "HealthEvent"
    expression = 'componentClass == "GPU" AND errorCode == "DCGM_FR_FAULTY_MEMORY" AND isFatal == true'
  
  [rule-sets.taint]
    key = "gpu.health/memory-error"
    value = "fatal"
    effect = "NoSchedule"
  
  [rule-sets.cordon]
    shouldCordon = true
```

### Standard Taint Examples

**Fatal GPU Error:**
```yaml
taints:
  - key: "gpu.health/memory-error"
    value: "fatal"
    effect: "NoSchedule"
```

**Thermal Warning:**
```yaml
taints:
  - key: "gpu.health/thermal-warning"
    value: "warning"
    effect: "PreferNoSchedule"
```

**NVLink Degraded:**
```yaml
taints:
  - key: "nvlink.health/link-down"
    value: "degraded"
    effect: "NoSchedule"
```

### Implementation Details

**Fault Quarantine Module** applies taints as follows:

1. Watches MongoDB Change Streams for new HealthEvents
2. Evaluates CEL rules against event + node context
3. If ruleset matches, applies configured taint via Kubernetes API
4. Cordons node if `shouldCordon: true`
5. Removes taints when healthy events arrive

**Configuration Location:** `distros/kubernetes/nvsentinel/charts/fault-quarantine/values.yaml`

## Labels

### Overview

Labels provide metadata about node health state, DCGM/driver versions, and configuration. These are informational and don't affect scheduling directly (unlike taints).

### Namespace Convention

All NVSentinel labels use the prefix: `nvsentinel.dgxc.nvidia.com/`

### Standard Labels

#### Health Metadata Labels

| Label Key | Value Format | Purpose | Set By |
|-----------|--------------|---------|--------|
| `nvsentinel.dgxc.nvidia.com/error-code` | DCGM error code string | Primary error code from health event | Platform Connectors |
| `nvsentinel.dgxc.nvidia.com/recommended-action` | RecommendedAction enum | Suggested remediation | Platform Connectors |
| `nvsentinel.dgxc.nvidia.com/quarantine-status` | `cordoned`\|`drained`\|`remediation-triggered` | Current state in remediation workflow | Multiple modules |
| `nvsentinel.dgxc.nvidia.com/last-health-check` | ISO8601 timestamp | Last health monitor check time | Platform Connectors |
| `nvsentinel.dgxc.nvidia.com/health-event-id` | MongoDB ObjectID | Correlation ID to event in database | Platform Connectors |

#### Configuration Labels

| Label Key | Value Format | Purpose | Set By |
|-----------|--------------|---------|--------|
| `nvsentinel.dgxc.nvidia.com/dcgm.version` | Semantic version (e.g., `3.3.5`) | DCGM version running on node | Labeler |
| `nvsentinel.dgxc.nvidia.com/driver.installed` | `true`\|`false` | Whether NVIDIA driver is installed | Labeler |
| `nvsentinel.dgxc.nvidia.com/kata.enabled` | `true`\|`false` | Whether Kata Containers runtime is enabled | Labeler |

#### Exclusion Labels

| Label Key | Value | Purpose | Set By |
|-----------|-------|---------|--------|
| `k8saas.nvidia.com/ManagedByNVSentinel` | `false` | Opt-out from NVSentinel management | Cluster operator (manual) |

### Label Lifecycle

**Labeler Module** manages configuration labels:
- Watches pod events to detect DCGM pod scheduling
- Extracts DCGM version from pod image tag
- Sets driver.installed based on driver probe results
- Watches node events for Kata runtime detection

**Platform Connectors** manages health metadata labels:
- Sets labels when processing HealthEvents
- Updates quarantine-status as workflow progresses

**Code Locations:**
- `labeler/pkg/labeler/labeler.go`
- `platform-connectors/pkg/connectors/kubernetes/process_node_events.go`

## Error Code Mapping Reference

NVSentinel maps DCGM error codes to recommended actions using a canonical CSV file.

**Mapping File:** `distros/kubernetes/nvsentinel/charts/gpu-health-monitor/files/dcgmerrorsmapping.csv`

### Recommended Actions

| Action | Meaning | Typical Resolution |
|--------|---------|-------------------|
| `RESTART_VM` | Software-recoverable error | Node reboot via janitor |
| `COMPONENT_RESET` | Hardware reset required | GPU/driver reset |
| `CONTACT_SUPPORT` | Manual intervention needed | Create support ticket, manual investigation |
| `IGNORE` | Informational, no action needed | Log for awareness |
| `NONE` | Health check informational | No action required |

### Example Mappings

| DCGM Error Code | Recommended Action | Typical Condition | Typical Taint |
|-----------------|-------------------|-------------------|---------------|
| `DCGM_FR_FAULTY_MEMORY` | `CONTACT_SUPPORT` | `GpuMemoryError` | `gpu.health/memory-error` |
| `DCGM_FR_VOLATILE_DBE_DETECTED` | `COMPONENT_RESET` | `GpuMemoryError` | `gpu.health/memory-error` |
| `DCGM_FR_NVLINK_DOWN` | `RESTART_VM` | `NVLinkDown` | `nvlink.health/link-down` |
| `DCGM_FR_NVSWITCH_FATAL_ERROR` | `CONTACT_SUPPORT` | `NVSwitchFatalError` | `nvswitch.health/fatal-error` |
| `DCGM_FR_CLOCK_THROTTLE_THERMAL` | `IGNORE` | `GpuThermalWatch` | (optional) `gpu.health/thermal-warning` |
| `DCGM_FR_SXID_ERROR` | `RESTART_VM` | `GpuXidError` | `gpu.health/xid-error` |

Full mapping contains 121 error codes. See CSV file for complete reference.

## Integration Patterns

### Watching for Health Changes

**Monitor specific condition types:**
```bash
kubectl get nodes -o json | jq '.items[] | select(.status.conditions[] | select(.type=="GpuMemoryError" and .status=="True")) | .metadata.name'
```

**Watch for condition changes:**
```bash
kubectl get nodes -w -o json | jq -c 'select(.status.conditions[] | select(.type | startswith("Gpu")))'
```

**Using client-go (Go):**
```go
informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
    UpdateFunc: func(oldObj, newObj interface{}) {
        newNode := newObj.(*corev1.Node)
        for _, condition := range newNode.Status.Conditions {
            if strings.HasPrefix(string(condition.Type), "Gpu") && condition.Status == corev1.ConditionTrue {
                // Handle GPU health issue
            }
        }
    },
})
```

### Tolerating Specific Errors

**Allow pods on nodes with GPU warnings:**
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-workload
spec:
  tolerations:
    - key: "gpu.health/thermal-warning"
      operator: "Equal"
      value: "warning"
      effect: "PreferNoSchedule"
```

**Tolerate all GPU health issues (not recommended for production):**
```yaml
tolerations:
  - key: "gpu.health"
    operator: "Exists"
```

### Querying Node Health Status

**Find nodes with fatal errors:**
```bash
kubectl get nodes -l 'nvsentinel.dgxc.nvidia.com/recommended-action=CONTACT_SUPPORT'
```

**Find cordoned nodes:**
```bash
kubectl get nodes -l 'nvsentinel.dgxc.nvidia.com/quarantine-status=cordoned'
```

**Find nodes with specific DCGM version:**
```bash
kubectl get nodes -l 'nvsentinel.dgxc.nvidia.com/dcgm.version=3.3.5'
```

### Filtering by Severity

**Find nodes with fatal taints:**
```bash
kubectl get nodes -o json | jq '.items[] | select(.spec.taints[]? | select(.value=="fatal")) | .metadata.name'
```

**Find nodes with degraded state:**
```bash
kubectl get nodes -o json | jq '.items[] | select(.spec.taints[]? | select(.value=="degraded")) | .metadata.name'
```

### External Scheduler Integration

**Custom scheduler predicate:**
```go
func filterNodesWithGPUErrors(node *corev1.Node) bool {
    // Check for GPU error conditions
    for _, condition := range node.Status.Conditions {
        if strings.HasPrefix(string(condition.Type), "Gpu") && 
           condition.Status == corev1.ConditionTrue {
            return false  // Filter out nodes with GPU errors
        }
    }
    
    // Check for fatal taints
    for _, taint := range node.Spec.Taints {
        if strings.HasPrefix(taint.Key, "gpu.health/") && 
           taint.Value == "fatal" {
            return false
        }
    }
    
    return true
}
```

### Monitoring Dashboard Queries

**Prometheus metrics example (if node-exporter is running):**
```promql
# Nodes with GPU errors
count(kube_node_status_condition{condition=~"Gpu.*", status="true"})

# Nodes cordoned by NVSentinel
count(kube_node_labels{label_nvsentinel_dgxc_nvidia_com_quarantine_status="cordoned"})
```

## Evolution and Versioning

### Stability Guarantees

**Node Condition Types:**
- ✅ **Stable:** New condition types may be added in minor versions
- ❌ **Never Removed:** Condition types are never removed or renamed
- 🔄 **Deprecation:** If a check is replaced, both old and new conditions are set for 2 releases

**Taint Keys:**
- ✅ **Prefix Stability:** The `component.health/` structure is stable
- 🔄 **Key Evolution:** New keys added, old keys deprecated with 2 version grace period
- ⚠️ **Effect Changes:** Operators can change effects via configuration (not breaking)

**Label Keys:**
- ✅ **Namespace Protection:** `nvsentinel.dgxc.nvidia.com/` prefix prevents conflicts
- 🔄 **Additive Only:** New labels added, old labels may be deprecated but not removed

### Version Compatibility

| NVSentinel Version | Condition API Version | Taint API Version | Label API Version |
|-------------------|----------------------|-------------------|-------------------|
| v0.1.x - v0.2.x | v1alpha1 | v1alpha1 | v1alpha1 |
| v0.3.x+ (future) | v1beta1 | v1beta1 | v1beta1 |

### Deprecation Policy

When a condition, taint, or label needs to change:

1. **Announce** deprecation in release notes
2. **Support** both old and new for 2 minor versions
3. **Remove** old after 2 version grace period
4. **Document** migration path in upgrade guide

## Examples

### Example 1: Node with Fatal GPU Memory Error

```yaml
apiVersion: v1
kind: Node
metadata:
  name: gpu-node-01
  labels:
    nvsentinel.dgxc.nvidia.com/error-code: "DCGM_FR_FAULTY_MEMORY"
    nvsentinel.dgxc.nvidia.com/recommended-action: "CONTACT_SUPPORT"
    nvsentinel.dgxc.nvidia.com/quarantine-status: "cordoned"
    nvsentinel.dgxc.nvidia.com/dcgm.version: "3.3.5"
    nvsentinel.dgxc.nvidia.com/driver.installed: "true"
    nvsentinel.dgxc.nvidia.com/health-event-id: "673bac8e9f1234567890abcd"
    nvsentinel.dgxc.nvidia.com/last-health-check: "2025-11-06T10:05:00Z"
spec:
  unschedulable: true  # Cordoned
  taints:
    - key: "gpu.health/memory-error"
      value: "fatal"
      effect: "NoSchedule"
status:
  conditions:
    - type: Ready
      status: "False"
      reason: "GpuHealthCheckFailed"
      message: "GPU health check failed"
    - type: GpuMemoryError
      status: "True"
      reason: "HardwareFailure"
      message: "[DCGM_FR_FAULTY_MEMORY] GPU memory failure detected on GPU 0 - RecommendedAction: CONTACT_SUPPORT"
      lastTransitionTime: "2025-11-06T10:00:00Z"
```

### Example 2: Node with Multiple Non-Fatal Warnings

```yaml
apiVersion: v1
kind: Node
metadata:
  name: gpu-node-02
  labels:
    nvsentinel.dgxc.nvidia.com/dcgm.version: "3.3.5"
    nvsentinel.dgxc.nvidia.com/driver.installed: "true"
spec:
  taints:
    - key: "gpu.health/thermal-warning"
      value: "warning"
      effect: "PreferNoSchedule"
status:
  conditions:
    - type: Ready
      status: "True"
    - type: GpuThermalWatch
      status: "True"
      reason: "ThermalThrottling"
      message: "[DCGM_FR_CLOCK_THROTTLE_THERMAL] GPU thermal throttling detected - RecommendedAction: IGNORE"
      lastTransitionTime: "2025-11-06T10:02:00Z"
    - type: GpuPowerWatch
      status: "False"
      reason: "PowerNormal"
      message: "GPU power within normal range"
      lastTransitionTime: "2025-11-06T09:00:00Z"
```

### Example 3: Healthy Node with NVSentinel Labels

```yaml
apiVersion: v1
kind: Node
metadata:
  name: gpu-node-03
  labels:
    nvsentinel.dgxc.nvidia.com/dcgm.version: "3.3.5"
    nvsentinel.dgxc.nvidia.com/driver.installed: "true"
    nvsentinel.dgxc.nvidia.com/kata.enabled: "false"
    nvsentinel.dgxc.nvidia.com/last-health-check: "2025-11-06T10:10:00Z"
status:
  conditions:
    - type: Ready
      status: "True"
    - type: GpuMemoryError
      status: "False"
      reason: "HealthCheckPassed"
      message: "GPU memory health check passed"
      lastTransitionTime: "2025-11-06T10:10:00Z"
    - type: GpuThermalWatch
      status: "False"
      reason: "HealthCheckPassed"
      message: "GPU thermal health check passed"
      lastTransitionTime: "2025-11-06T10:10:00Z"
```

## Implementation Notes

### Module Responsibilities

| Module | Responsibility | What It Sets |
|--------|---------------|--------------|
| **Platform Connectors** | Process health events, update node status | NodeConditions, health metadata labels |
| **Fault Quarantine** | Apply operational policies | Taints, cordon status, quarantine-status label |
| **Labeler** | Maintain configuration metadata | DCGM version, driver status, kata labels |
| **Node Drainer** | Evict workloads | Updates quarantine-status label to `drained` |
| **Fault Remediation** | Trigger maintenance | Updates quarantine-status label to `remediation-triggered` |

### Configuration Files

- **Error Mapping:** `distros/kubernetes/nvsentinel/charts/gpu-health-monitor/files/dcgmerrorsmapping.csv`
- **Quarantine Rules:** `distros/kubernetes/nvsentinel/charts/fault-quarantine/values.yaml`
- **Module Config:** `distros/kubernetes/nvsentinel/values.yaml`

### Code Locations

- **Condition Setting:** `platform-connectors/pkg/connectors/kubernetes/process_node_events.go`
- **Taint Application:** `fault-quarantine/pkg/informer/k8s_client.go`
- **Label Management:** `labeler/pkg/labeler/labeler.go`

## Related Documentation

- [ADR-003: Rule-Based Node Quarantine](./designs/003-rule-based-node-quarantine.md) - CEL-based quarantine rules
- [ADR-009: Fault Remediation Triggering](./designs/009-fault-remediation-triggering.md) - Remediation workflow
- [Data Flow Documentation](./DATA_FLOW.md) - End-to-end event flow
- [Helm Chart Configuration](../distros/kubernetes/README.md) - Deployment configuration

## Contributing

This document describes the stable API contract for NVSentinel node health signaling. Changes to condition types, taint keys, or label keys require review and follow the deprecation policy.

To propose changes:
1. Open an issue describing the use case
2. Discuss impact on external integrations
3. Follow the versioning and deprecation guidelines
4. Update this document as part of the PR

---

**Document Revision History:**

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2025-11-06 | Initial version documenting existing conventions |
