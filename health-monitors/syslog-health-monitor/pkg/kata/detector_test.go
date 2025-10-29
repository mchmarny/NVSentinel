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
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDetector_IsKataEnabled_FilesystemDetection(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) string // Returns temp dir path
		expectedResult bool
		wantErr        bool
	}{
		{
			name: "no kata indicators - should return false",
			setupFunc: func(t *testing.T) string {
				return t.TempDir()
			},
			expectedResult: false,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			detector := NewDetector("test-node", nil)

			got, err := detector.IsKataEnabled(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsKataEnabled() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expectedResult {
				t.Errorf("IsKataEnabled() = %v, want %v", got, tt.expectedResult)
			}
		})
	}
}

func TestDetector_checkCgroupForKata(t *testing.T) {
	tests := []struct {
		name           string
		cgroupContent  string
		expectedResult bool
		wantErr        bool
	}{
		{
			name: "kata detected in cgroup path",
			cgroupContent: `12:perf_event:/kata-containers/abc123
11:hugetlb:/kata-containers/abc123
10:freezer:/kata-containers/abc123`,
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "kata-runtime in cgroup",
			cgroupContent: `12:perf_event:/system.slice/kata-runtime.service
11:hugetlb:/system.slice/kata-runtime.service`,
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "io.katacontainers in cgroup",
			cgroupContent: `0::/io.katacontainers.sandbox/abc123
1:name=systemd:/io.katacontainers.sandbox/abc123`,
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "regular containerd cgroup - no kata",
			cgroupContent: `12:perf_event:/system.slice/containerd.service
11:hugetlb:/system.slice/containerd.service
10:freezer:/kubepods/besteffort/pod123`,
			expectedResult: false,
			wantErr:        false,
		},
		{
			name: "docker cgroup - no kata",
			cgroupContent: `12:perf_event:/docker/abc123
11:hugetlb:/docker/abc123`,
			expectedResult: false,
			wantErr:        false,
		},
		{
			name:           "empty cgroup file",
			cgroupContent:  "",
			expectedResult: false,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary cgroup file
			tmpDir := t.TempDir()
			cgroupPath := filepath.Join(tmpDir, "cgroup")

			if err := os.WriteFile(cgroupPath, []byte(tt.cgroupContent), 0600); err != nil {
				t.Fatalf("Failed to create test cgroup file: %v", err)
			}

			detector := NewDetector("test-node", nil)
			got, err := detector.checkCgroupForKata(cgroupPath)

			if (err != nil) != tt.wantErr {
				t.Errorf("checkCgroupForKata() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expectedResult {
				t.Errorf("checkCgroupForKata() = %v, want %v", got, tt.expectedResult)
			}
		})
	}
}

func TestDetector_checkForVMHypervisor(t *testing.T) {
	tests := []struct {
		name           string
		cpuinfoContent string
		expectedResult bool
		wantErr        bool
	}{
		{
			name: "hypervisor flag present",
			cpuinfoContent: `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 85
flags		: fpu vme de pse tsc msr pae hypervisor constant_tsc`,
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "qemu hypervisor detected",
			cpuinfoContent: `processor	: 0
vendor_id	: GenuineIntel
model name	: QEMU Virtual CPU version 2.5+`,
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "kvm detected",
			cpuinfoContent: `processor	: 0
hypervisor	: KVM
vendor_id	: GenuineIntel`,
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "physical hardware - no hypervisor",
			cpuinfoContent: `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 85
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep`,
			expectedResult: false,
			wantErr:        false,
		},
		{
			name:           "empty cpuinfo",
			cpuinfoContent: "",
			expectedResult: false,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary cpuinfo file
			tmpDir := t.TempDir()
			cpuinfoPath := filepath.Join(tmpDir, "cpuinfo")

			if err := os.WriteFile(cpuinfoPath, []byte(tt.cpuinfoContent), 0600); err != nil {
				t.Fatalf("Failed to create test cpuinfo file: %v", err)
			}

			detector := NewDetector("test-node", nil)
			got, err := detector.checkForVMHypervisor(cpuinfoPath)

			if (err != nil) != tt.wantErr {
				t.Errorf("checkForVMHypervisor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expectedResult {
				t.Errorf("checkForVMHypervisor() = %v, want %v", got, tt.expectedResult)
			}
		})
	}
}

func TestDetector_detectViaKubernetesAPI(t *testing.T) {
	tests := []struct {
		name           string
		node           *corev1.Node
		expectedResult bool
		wantErr        bool
	}{
		{
			name: "kata detected in runtime version",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "containerd://1.6.2-kata",
					},
				},
			},
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "kata detected via node label - true value",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"katacontainers.io/kata-runtime": "true",
					},
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "containerd://1.6.2",
					},
				},
			},
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "kata detected via node label - enabled value",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"kata-containers.io/runtime": "enabled",
					},
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "containerd://1.6.2",
					},
				},
			},
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "kata detected via annotation",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						"kata-runtime.io/enabled": "true",
					},
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "containerd://1.6.2",
					},
				},
			},
			expectedResult: true,
			wantErr:        false,
		},
		{
			name: "regular containerd - no kata",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "containerd://1.6.2",
					},
				},
			},
			expectedResult: false,
			wantErr:        false,
		},
		{
			name: "docker runtime - no kata",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "docker://20.10.7",
					},
				},
			},
			expectedResult: false,
			wantErr:        false,
		},
		{
			name: "kata label with false value - not enabled",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"katacontainers.io/kata-runtime": "false",
					},
				},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{
						ContainerRuntimeVersion: "containerd://1.6.2",
					},
				},
			},
			expectedResult: false,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			clientset := fake.NewSimpleClientset(tt.node)
			detector := NewDetector("test-node", clientset)

			got, err := detector.detectViaKubernetesAPI(ctx)

			if (err != nil) != tt.wantErr {
				t.Errorf("detectViaKubernetesAPI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expectedResult {
				t.Errorf("detectViaKubernetesAPI() = %v, want %v", got, tt.expectedResult)
			}
		})
	}
}

func TestDetector_detectViaKubernetesAPI_NodeNotFound(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset() // Empty clientset
	detector := NewDetector("non-existent-node", clientset)

	_, err := detector.detectViaKubernetesAPI(ctx)
	if err == nil {
		t.Error("detectViaKubernetesAPI() expected error for non-existent node, got nil")
	}
}

func TestNewDetector(t *testing.T) {
	nodeName := "test-node"
	clientset := fake.NewSimpleClientset()

	detector := NewDetector(nodeName, clientset)

	if detector == nil {
		t.Fatal("NewDetector() returned nil")
	}
	if detector.nodeName != nodeName {
		t.Errorf("NewDetector() nodeName = %v, want %v", detector.nodeName, nodeName)
	}
	if detector.clientset != clientset {
		t.Error("NewDetector() clientset not set correctly")
	}
}

func TestDetector_IsKataEnabled_WithAPIFallback(t *testing.T) {
	ctx := context.Background()

	// Create a node with kata runtime
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				ContainerRuntimeVersion: "containerd://1.6.2-kata",
			},
		},
	}

	clientset := fake.NewSimpleClientset(node)
	detector := NewDetector("test-node", clientset)

	// Even if filesystem detection fails, API should detect it
	got, err := detector.IsKataEnabled(ctx)
	if err != nil {
		t.Errorf("IsKataEnabled() unexpected error: %v", err)
	}
	if !got {
		t.Error("IsKataEnabled() should detect kata via API fallback")
	}
}

func TestDetector_IsKataEnabled_NoClientset(t *testing.T) {
	ctx := context.Background()

	// Detector without clientset should only use filesystem detection
	detector := NewDetector("test-node", nil)

	// Should not error, just return false when no indicators found
	got, err := detector.IsKataEnabled(ctx)
	if err != nil {
		t.Errorf("IsKataEnabled() unexpected error: %v", err)
	}
	if got {
		t.Error("IsKataEnabled() should return false when no kata indicators found")
	}
}

func TestNewDetectorWithTimeout(t *testing.T) {
	customTimeout := 10 * time.Second
	nodeName := "test-node"
	clientset := fake.NewSimpleClientset()

	detector := NewDetectorWithTimeout(nodeName, clientset, customTimeout)

	if detector == nil {
		t.Fatal("NewDetectorWithTimeout() returned nil")
	}
	if detector.nodeName != nodeName {
		t.Errorf("NewDetectorWithTimeout() nodeName = %v, want %v", detector.nodeName, nodeName)
	}
	if detector.clientset != clientset {
		t.Error("NewDetectorWithTimeout() clientset not set correctly")
	}
	if detector.detectionTimeout != customTimeout {
		t.Errorf("NewDetectorWithTimeout() timeout = %v, want %v", detector.detectionTimeout, customTimeout)
	}
}

func TestNewDetector_UsesDefaultTimeout(t *testing.T) {
	nodeName := "test-node"
	clientset := fake.NewSimpleClientset()

	detector := NewDetector(nodeName, clientset)

	if detector == nil {
		t.Fatal("NewDetector() returned nil")
	}
	if detector.detectionTimeout != DefaultDetectionTimeout {
		t.Errorf("NewDetector() timeout = %v, want %v", detector.detectionTimeout, DefaultDetectionTimeout)
	}
}
