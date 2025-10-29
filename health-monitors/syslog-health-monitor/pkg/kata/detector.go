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
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	// DefaultDetectionTimeout is the default timeout for detection operations
	DefaultDetectionTimeout = 5 * time.Second
)

// DetectionMethod represents the method used for Kata detection
type DetectionMethod string

const (
	DetectionMethodKubernetesAPI DetectionMethod = "kubernetes-api"
	DetectionMethodNone          DetectionMethod = "none"
)

// DetectionResult provides detailed information about the detection attempt
type DetectionResult struct {
	IsKata           bool
	Method           DetectionMethod
	AttemptedMethods []DetectionMethod
	Errors           []error
}

// Detector provides methods to detect if the current node is running Kata Containers
type Detector struct {
	nodeName         string
	clientset        kubernetes.Interface
	detectionTimeout time.Duration
	enableMetrics    bool
}

// DetectorOption is a functional option for configuring the Detector
type DetectorOption func(*Detector)

// WithTimeout sets a custom detection timeout
func WithTimeout(timeout time.Duration) DetectorOption {
	return func(d *Detector) {
		d.detectionTimeout = timeout
	}
}

// WithMetrics enables or disables Prometheus metrics collection
func WithMetrics(enabled bool) DetectorOption {
	return func(d *Detector) {
		d.enableMetrics = enabled
	}
}

// NewDetector creates a Kata runtime detector with default configuration.
// Returns error if node name violates DNS-1123 subdomain rules.
func NewDetector(nodeName string, clientset kubernetes.Interface, opts ...DetectorOption) (*Detector, error) {
	// Validate node name according to Kubernetes DNS-1123 subdomain spec
	if errs := validation.IsDNS1123Subdomain(nodeName); len(errs) > 0 {
		return nil, fmt.Errorf("invalid node name %q: %v", nodeName, errs)
	}

	d := &Detector{
		nodeName:         nodeName,
		clientset:        clientset,
		detectionTimeout: DefaultDetectionTimeout,
		enableMetrics:    true, // Enable by default
	}

	// Apply options
	for _, opt := range opts {
		opt(d)
	}

	return d, nil
}

// NewDetectorWithTimeout creates Kata detector with custom timeout.
// Deprecated: Use NewDetector with WithTimeout option.
func NewDetectorWithTimeout(nodeName string, clientset kubernetes.Interface, timeout time.Duration) *Detector {
	detector, err := NewDetector(nodeName, clientset, WithTimeout(timeout))
	if err != nil {
		// For backward compatibility, log error but don't fail
		slog.Warn("Failed to create detector with validation", "error", err, "node", nodeName)
		// Return detector without validation (old behavior)
		return &Detector{
			nodeName:         nodeName,
			clientset:        clientset,
			detectionTimeout: timeout,
			enableMetrics:    true,
		}
	}

	return detector
}

// IsKataEnabled determines if Kata Containers runtime is enabled on this node.
// Checks node metadata including:
// 1. Container runtime version
// 2. Node labels
// 3. Node annotations
//
// Filesystem detection intentionally excluded - binaries/directories persist after
// Kata is disabled, causing false positives. Only API reflects active config.
//
// RuntimeClass detection is not used because RuntimeClass resources are cluster-wide
// and don't indicate which specific nodes support the runtime handler.
//
// Returns DetectionResult with attempted methods and any errors encountered.
func (d *Detector) IsKataEnabled(ctx context.Context) (*DetectionResult, error) {
	// Respect parent context cancellation
	if err := d.checkContextCancellation(ctx); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, d.detectionTimeout)
	defer cancel()

	result := &DetectionResult{
		IsKata:           false,
		Method:           DetectionMethodNone,
		AttemptedMethods: make([]DetectionMethod, 0, 1),
		Errors:           make([]error, 0, 1),
	}

	slog.Info("Detecting Kata Containers runtime via Kubernetes API", "node", d.nodeName)

	// Validate prerequisites
	if err := d.validateClientset(); err != nil {
		return nil, err
	}

	// Fetch node for metadata detection
	node, err := d.getNode(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Check node metadata (runtime version, labels, annotations)
	result.AttemptedMethods = append(result.AttemptedMethods, DetectionMethodKubernetesAPI)
	start := time.Now()
	if d.checkNodeMetadata(node) {
		result.IsKata = true
		result.Method = DetectionMethodKubernetesAPI
		duration := time.Since(start)

		if d.enableMetrics && metricsEnabled {
			detectionAttempts.WithLabelValues(d.nodeName, string(DetectionMethodKubernetesAPI), "true").Inc()
			detectionDuration.WithLabelValues(d.nodeName, string(DetectionMethodKubernetesAPI), "true").Observe(duration.Seconds())
		}

		slog.Info("Kata Containers detected via Kubernetes API", "node", d.nodeName)
		d.recordDetectionResult(result)
		return result, nil
	}

	if d.enableMetrics && metricsEnabled {
		duration := time.Since(start)
		detectionAttempts.WithLabelValues(d.nodeName, string(DetectionMethodKubernetesAPI), "true").Inc()
		detectionDuration.WithLabelValues(d.nodeName, string(DetectionMethodKubernetesAPI), "false").Observe(duration.Seconds())
	}

	// Record final result
	d.recordDetectionResult(result)

	return result, nil
}

// checkContextCancellation checks if the parent context is already cancelled
func (d *Detector) checkContextCancellation(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// validateClientset ensures the Kubernetes clientset is available
func (d *Detector) validateClientset() error {
	if d.clientset == nil {
		slog.Error("Detection failed: no Kubernetes API access", "node", d.nodeName)

		return fmt.Errorf("kubernetes clientset required for kata detection")
	}

	return nil
}

// recordDetectionResult logs and records metrics for the final detection result
func (d *Detector) recordDetectionResult(result *DetectionResult) {
	if d.enableMetrics && metricsEnabled {
		detectionResults.WithLabelValues(d.nodeName, fmt.Sprintf("%t", result.IsKata)).Inc()
	}

	if !result.IsKata {
		slog.Info("Kata Containers not detected, using standard runtime",
			"node", d.nodeName,
			"attempted_methods", result.AttemptedMethods,
			"errors", len(result.Errors))
	}
}

// getNode retrieves node object via API with retry on transient errors.
func (d *Detector) getNode(ctx context.Context) (*corev1.Node, error) {
	var node *corev1.Node

	retryErr := retry.OnError(
		retry.DefaultBackoff,
		func(err error) bool {
			// Retry on transient errors
			return apierrors.IsServerTimeout(err) ||
				apierrors.IsServiceUnavailable(err) ||
				apierrors.IsTooManyRequests(err) ||
				apierrors.IsTimeout(err)
		},
		func() error {
			var err error
			node, err = d.clientset.CoreV1().Nodes().Get(ctx, d.nodeName, metav1.GetOptions{})

			return err
		},
	)

	if retryErr != nil {
		return nil, fmt.Errorf("failed to get node after retries: %w", retryErr)
	}

	return node, nil
}

// checkNodeMetadata examines node metadata for Kata indicators.
func (d *Detector) checkNodeMetadata(node *corev1.Node) bool {
	// Check 1: Examine container runtime version
	if d.checkRuntimeVersion(node) {
		return true
	}

	// Check 2: Look for Kata-related node labels
	if d.checkNodeLabels(node) {
		return true
	}

	// Check 3: Examine node annotations for Kata configuration
	return d.checkNodeAnnotations(node)
}

// isTruthyValue checks if a string value represents a truthy state.
// Returns true for: "true", "enabled", "1", "yes" (case-insensitive).
func isTruthyValue(value string) bool {
	lowerValue := strings.ToLower(value)
	return lowerValue == "true" || lowerValue == "enabled" || lowerValue == "1" || lowerValue == "yes"
}

// checkRuntimeVersion examines container runtime version for Kata indicators.
func (d *Detector) checkRuntimeVersion(node *corev1.Node) bool {
	runtime := strings.ToLower(node.Status.NodeInfo.ContainerRuntimeVersion)
	if strings.Contains(runtime, "kata") {
		slog.Debug("Kata detected in container runtime version", "runtime", runtime)
		return true
	}

	return false
}

// checkNodeLabels looks for Kata-related node labels.
func (d *Detector) checkNodeLabels(node *corev1.Node) bool {
	kataLabels := []string{
		"katacontainers.io/kata-runtime",
		"kata-containers.io/runtime",
		"node.kubernetes.io/kata-enabled",
		"kata.io/enabled",
		"runtime.kata",
	}

	for _, label := range kataLabels {
		value, exists := node.Labels[label]
		if !exists {
			continue
		}

		// Check if label value indicates enabled ("true", "enabled", "1", etc.)
		if isTruthyValue(value) {
			slog.Debug("Kata detected via node label", "label", label, "value", value)
			return true
		}
	}

	return false
}

// checkNodeAnnotations examines node annotations for Kata configuration.
func (d *Detector) checkNodeAnnotations(node *corev1.Node) bool {
	kataAnnotations := []string{
		"kata-runtime.io/enabled",
		"io.katacontainers.config",
	}

	for _, annotation := range kataAnnotations {
		value, exists := node.Annotations[annotation]
		if !exists || value == "" {
			continue
		}

		// Validate that the annotation value indicates enabled state
		if isTruthyValue(value) {
			slog.Debug("Kata detected via node annotation", "annotation", annotation, "value", value)
			return true
		}
	}

	return false
}
