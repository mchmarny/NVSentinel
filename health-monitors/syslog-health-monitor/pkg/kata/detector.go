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
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// DefaultDetectionTimeout is the default timeout for detection operations
	DefaultDetectionTimeout = 5 * time.Second
)

// Detector provides methods to detect if the current node is running Kata Containers
type Detector struct {
	nodeName         string
	clientset        kubernetes.Interface
	detectionTimeout time.Duration
}

// NewDetector creates a new Kata runtime detector with default timeout
func NewDetector(nodeName string, clientset kubernetes.Interface) *Detector {
	return NewDetectorWithTimeout(nodeName, clientset, DefaultDetectionTimeout)
}

// NewDetectorWithTimeout creates a new Kata runtime detector with custom timeout
func NewDetectorWithTimeout(nodeName string, clientset kubernetes.Interface, timeout time.Duration) *Detector {
	return &Detector{
		nodeName:         nodeName,
		clientset:        clientset,
		detectionTimeout: timeout,
	}
}

// IsKataEnabled determines if Kata Containers runtime is enabled on the current node.
// It uses multiple detection methods with fallback for reliability:
// 1. Filesystem detection (fastest, no API calls)
// 2. Kubernetes node status check (requires API access)
// 3. Node labels check (requires API access)
func (d *Detector) IsKataEnabled(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, d.detectionTimeout)
	defer cancel()

	slog.Info("Detecting Kata Containers runtime", "node", d.nodeName)

	// Method 1: Check host filesystem for Kata indicators (fastest method)
	if isKata, err := d.detectViaFilesystem(); err == nil {
		if isKata {
			slog.Info("Kata Containers detected via filesystem inspection", "node", d.nodeName)
			return true, nil
		}
	} else {
		slog.Warn("Filesystem-based Kata detection failed", "error", err)
	}

	// Method 2: Check via Kubernetes API if clientset is available
	if d.clientset != nil {
		if isKata, err := d.detectViaKubernetesAPI(ctx); err == nil {
			if isKata {
				slog.Info("Kata Containers detected via Kubernetes API", "node", d.nodeName)
				return true, nil
			}
		} else {
			slog.Warn("Kubernetes API-based Kata detection failed", "error", err)
		}
	}

	slog.Info("Kata Containers not detected, using standard runtime", "node", d.nodeName)
	return false, nil
}

// detectViaFilesystem checks the host filesystem for Kata-specific indicators
// This is the fastest method as it doesn't require API calls
func (d *Detector) detectViaFilesystem() (bool, error) {
	// Check 1: Look for Kata runtime binaries on the host
	kataBinaries := []string{
		"/opt/kata/bin/kata-runtime",
		"/usr/bin/kata-runtime",
		"/usr/local/bin/kata-runtime",
	}

	for _, binary := range kataBinaries {
		if _, err := os.Stat(binary); err == nil {
			slog.Debug("Kata runtime binary found", "path", binary)
			return true, nil
		}
	}

	// Check 2: Examine /proc/1/cgroup for kata indicators
	// In Kata containers, the cgroup hierarchy often contains "kata" references
	if isKata, err := d.checkCgroupForKata("/nvsentinel/proc/1/cgroup"); err == nil && isKata {
		slog.Debug("Kata detected in cgroup hierarchy")
		return true, nil
	}

	// Check 3: Look for Kata-specific directories
	kataDirectories := []string{
		"/run/kata-containers",
		"/var/run/kata-containers",
	}

	for _, dir := range kataDirectories {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			slog.Debug("Kata runtime directory found", "path", dir)
			return true, nil
		}
	}

	// Check 4: Check for hypervisor indicators in /proc/cpuinfo
	// Kata runs in a VM, so we might see hypervisor flags
	if isVM, err := d.checkForVMHypervisor("/nvsentinel/proc/cpuinfo"); err == nil && isVM {
		// This alone is not conclusive (node itself might be a VM)
		// but combined with container context, it's a strong indicator
		slog.Debug("VM hypervisor detected, possible Kata environment")
		// Don't return true here as this is not conclusive enough
	}

	return false, nil
}

// checkCgroupForKata examines the cgroup file for Kata-specific patterns
func (d *Detector) checkCgroupForKata(cgroupPath string) (bool, error) {
	file, err := os.Open(cgroupPath)
	if err != nil {
		return false, fmt.Errorf("failed to open cgroup file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.ToLower(scanner.Text())
		// Look for kata-specific cgroup identifiers
		if strings.Contains(line, "kata") ||
			strings.Contains(line, "kata-runtime") ||
			strings.Contains(line, "io.katacontainers") {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error reading cgroup file: %w", err)
	}

	return false, nil
}

// checkForVMHypervisor checks if the system is running under a hypervisor
func (d *Detector) checkForVMHypervisor(cpuinfoPath string) (bool, error) {
	file, err := os.Open(cpuinfoPath)
	if err != nil {
		return false, fmt.Errorf("failed to open cpuinfo: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.ToLower(scanner.Text())
		// Look for hypervisor flag in CPU flags
		if strings.Contains(line, "flags") && strings.Contains(line, "hypervisor") {
			return true, nil
		}
		// Check for specific hypervisor vendor strings
		if strings.Contains(line, "hypervisor") ||
			strings.Contains(line, "qemu") ||
			strings.Contains(line, "kvm") {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error reading cpuinfo: %w", err)
	}

	return false, nil
}

// detectViaKubernetesAPI uses the Kubernetes API to check node runtime and labels
func (d *Detector) detectViaKubernetesAPI(ctx context.Context) (bool, error) {
	node, err := d.clientset.CoreV1().Nodes().Get(ctx, d.nodeName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get node from Kubernetes API: %w", err)
	}

	// Check 1: Examine container runtime version
	runtime := strings.ToLower(node.Status.NodeInfo.ContainerRuntimeVersion)
	if strings.Contains(runtime, "kata") {
		slog.Debug("Kata detected in container runtime version", "runtime", runtime)
		return true, nil
	}

	// Check 2: Look for Kata-related node labels
	kataLabels := []string{
		"katacontainers.io/kata-runtime",
		"kata-containers.io/runtime",
		"node.kubernetes.io/kata-enabled",
		"kata.io/enabled",
		"runtime.kata",
	}

	for _, label := range kataLabels {
		if value, exists := node.Labels[label]; exists {
			// Check if label value indicates enabled (could be "true", "enabled", "1", etc.)
			lowerValue := strings.ToLower(value)
			if lowerValue == "true" || lowerValue == "enabled" || lowerValue == "1" || lowerValue == "yes" {
				slog.Debug("Kata detected via node label", "label", label, "value", value)
				return true, nil
			}
		}
	}

	// Check 3: Examine node annotations for Kata configuration
	kataAnnotations := []string{
		"kata-runtime.io/enabled",
		"io.katacontainers.config",
	}

	for _, annotation := range kataAnnotations {
		if value, exists := node.Annotations[annotation]; exists && value != "" {
			slog.Debug("Kata detected via node annotation", "annotation", annotation)
			return true, nil
		}
	}

	return false, nil
}
