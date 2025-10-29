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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDetector_IsKataEnabled_NoClientset(t *testing.T) {
	ctx := context.Background()

	// Detector without clientset should fail as API detection is required
	detector, err := NewDetector("test-node", nil, WithMetrics(false))
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Should error since filesystem detection is disabled
	_, err = detector.IsKataEnabled(ctx)
	if err == nil {
		t.Error("IsKataEnabled() should return error when no clientset provided")
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
			clientset := fake.NewSimpleClientset(tt.node)
			detector, err := NewDetector("test-node", clientset, WithMetrics(false))
			if err != nil {
				t.Fatalf("Failed to create detector: %v", err)
			}

			got := detector.checkNodeMetadata(tt.node)

			if got != tt.expectedResult {
				t.Errorf("checkNodeMetadata() = %v, want %v", got, tt.expectedResult)
			}
		})
	}
}

func TestDetector_detectViaKubernetesAPI_NodeNotFound(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset() // Empty clientset
	detector, err := NewDetector("non-existent-node", clientset, WithMetrics(false))
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	_, err = detector.getNode(ctx)
	if err == nil {
		t.Error("getNode() expected error for non-existent node, got nil")
	}
}

func TestNewDetector(t *testing.T) {
	nodeName := "test-node"
	clientset := fake.NewSimpleClientset()

	detector, err := NewDetector(nodeName, clientset, WithMetrics(false))
	if err != nil {
		t.Fatalf("NewDetector() failed: %v", err)
	}

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
	detector, err := NewDetector("test-node", clientset, WithMetrics(false))
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// API should detect it
	result, err := detector.IsKataEnabled(ctx)
	if err != nil {
		t.Errorf("IsKataEnabled() unexpected error: %v", err)
	}
	if !result.IsKata {
		t.Error("IsKataEnabled() should detect kata via API")
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

	detector, err := NewDetector(nodeName, clientset, WithMetrics(false))
	if err != nil {
		t.Fatalf("NewDetector() failed: %v", err)
	}

	if detector == nil {
		t.Fatal("NewDetector() returned nil")
	}
	if detector.detectionTimeout != DefaultDetectionTimeout {
		t.Errorf("NewDetector() timeout = %v, want %v", detector.detectionTimeout, DefaultDetectionTimeout)
	}
}

func TestNewDetector_WithOptions(t *testing.T) {
	nodeName := "test-node"
	clientset := fake.NewSimpleClientset()
	customTimeout := 10 * time.Second

	detector, err := NewDetector(
		nodeName,
		clientset,
		WithTimeout(customTimeout),
		WithMetrics(false),
	)
	if err != nil {
		t.Fatalf("NewDetector() failed: %v", err)
	}

	if detector.detectionTimeout != customTimeout {
		t.Errorf("WithTimeout() = %v, want %v", detector.detectionTimeout, customTimeout)
	}
	if detector.enableMetrics {
		t.Error("WithMetrics(false) should disable metrics")
	}
}

func TestNewDetector_InvalidNodeName(t *testing.T) {
	invalidNames := []string{
		"",
		"Node_With_Underscores",
		"node@with@special",
		"UPPERCASE",
		strings.Repeat("a", 254), // Too long
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			_, err := NewDetector(name, nil)
			if err == nil {
				t.Errorf("NewDetector(%q) should return error for invalid name", name)
			}
		})
	}
}

func TestNewDetector_ValidNodeName(t *testing.T) {
	validNames := []string{
		"test-node",
		"node-123",
		"my.node.example.com",
		"a",
		strings.Repeat("a", 253), // Max length
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			detector, err := NewDetector(name, nil, WithMetrics(false))
			if err != nil {
				t.Errorf("NewDetector(%q) unexpected error: %v", name, err)
			}
			if detector == nil {
				t.Errorf("NewDetector(%q) returned nil", name)
			}
		})
	}
}

func TestDetector_ContextCancellation(t *testing.T) {
	detector, err := NewDetector("test-node", nil, WithMetrics(false))
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = detector.IsKataEnabled(ctx)
	if err == nil {
		t.Error("IsKataEnabled() should return error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("IsKataEnabled() error = %v, want %v", err, context.Canceled)
	}
}

func TestCachedDetector(t *testing.T) {
	// Create a node without kata
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				ContainerRuntimeVersion: "containerd://1.6.2",
			},
		},
	}

	clientset := fake.NewSimpleClientset(node)
	detector, err := NewDetector("test-node", clientset, WithMetrics(false))
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	cached := NewCachedDetector(detector, 1*time.Second)

	// First call should perform detection
	result1, err := cached.IsKataEnabled(context.Background())
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}

	// Second call should return cached result
	result2, err := cached.IsKataEnabled(context.Background())
	if err != nil {
		t.Fatalf("Second call failed: %v", err)
	}

	if result1.IsKata != result2.IsKata {
		t.Error("Cached result differs from original")
	}

	// Invalidate cache
	cached.InvalidateCache()

	// Should perform detection again
	result3, err := cached.IsKataEnabled(context.Background())
	if err != nil {
		t.Fatalf("Third call failed: %v", err)
	}

	if result1.IsKata != result3.IsKata {
		t.Error("Result after invalidation differs")
	}
}

func TestCachedDetector_Expiration(t *testing.T) {
	// Create a node without kata
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				ContainerRuntimeVersion: "containerd://1.6.2",
			},
		},
	}

	clientset := fake.NewSimpleClientset(node)
	detector, err := NewDetector("test-node", clientset, WithMetrics(false))
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Very short TTL for testing
	cached := NewCachedDetector(detector, 10*time.Millisecond)

	// First call
	_, err = cached.IsKataEnabled(context.Background())
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}

	// Wait for cache to expire
	time.Sleep(20 * time.Millisecond)

	// Should perform detection again (cache expired)
	_, err = cached.IsKataEnabled(context.Background())
	if err != nil {
		t.Fatalf("Second call failed: %v", err)
	}
}

func TestDetector_DetectionResult(t *testing.T) {
	// Create a node without kata
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				ContainerRuntimeVersion: "containerd://1.6.2",
			},
		},
	}

	clientset := fake.NewSimpleClientset(node)
	detector, err := NewDetector("test-node", clientset, WithMetrics(false))
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	result, err := detector.IsKataEnabled(context.Background())
	if err != nil {
		t.Fatalf("Detection failed: %v", err)
	}

	// Check result structure
	if result == nil {
		t.Fatal("Result should not be nil")
	}

	// Should have attempted API detection methods
	if len(result.AttemptedMethods) == 0 {
		t.Error("No detection methods were attempted")
	}

	// Method should be set appropriately
	if result.IsKata && result.Method == DetectionMethodNone {
		t.Error("Kata detected but method is 'none'")
	}
}

func TestDetector_RuntimeClassCaching(t *testing.T) {
	// Create a RuntimeClass with kata handler
	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kata-qemu",
		},
		Handler: "kata-qemu",
	}

	clientset := fake.NewSimpleClientset(rc)
	detector, err := NewDetector("test-node", clientset,
		WithMetrics(false),
		WithRuntimeClassCacheTTL(100*time.Millisecond))
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// First call should query API
	start := time.Now()
	isKata1, err := detector.detectViaRuntimeClass(context.Background())
	duration1 := time.Since(start)
	if err != nil {
		t.Fatalf("First detection failed: %v", err)
	}
	if !isKata1 {
		t.Error("Expected Kata to be detected")
	}

	// Second call should use cache (much faster)
	start = time.Now()
	isKata2, err := detector.detectViaRuntimeClass(context.Background())
	duration2 := time.Since(start)
	if err != nil {
		t.Fatalf("Second detection failed: %v", err)
	}
	if !isKata2 {
		t.Error("Expected Kata to be detected from cache")
	}

	// Cache should be faster (though in tests both are fast due to fake client)
	t.Logf("First call: %v, Second call (cached): %v", duration1, duration2)

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Third call should query API again
	isKata3, err := detector.detectViaRuntimeClass(context.Background())
	if err != nil {
		t.Fatalf("Third detection failed: %v", err)
	}
	if !isKata3 {
		t.Error("Expected Kata to be detected after cache expiry")
	}
}

func TestDetector_WithRuntimeClassCacheTTL(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	customTTL := 1 * time.Hour

	detector, err := NewDetector("test-node", clientset,
		WithMetrics(false),
		WithRuntimeClassCacheTTL(customTTL))
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	if detector.rcCache.ttl != customTTL {
		t.Errorf("Expected cache TTL %v, got %v", customTTL, detector.rcCache.ttl)
	}
}
