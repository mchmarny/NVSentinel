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
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
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

	// DefaultRuntimeClassCacheTTL is the default TTL for RuntimeClass cache
	// RuntimeClasses rarely change after cluster setup, so a longer cache is reasonable
	DefaultRuntimeClassCacheTTL = 10 * time.Minute
)

// DetectionMethod represents the method used for Kata detection
type DetectionMethod string

const (
	DetectionMethodKubernetesAPI DetectionMethod = "kubernetes-api"
	DetectionMethodRuntimeClass  DetectionMethod = "runtime-class"
	DetectionMethodNone          DetectionMethod = "none"
)

// DetectionResult provides detailed information about the detection attempt
type DetectionResult struct {
	mu               sync.Mutex
	IsKata           bool
	Method           DetectionMethod
	AttemptedMethods []DetectionMethod
	Errors           []error
}

// runtimeClassCache provides TTL-based caching for RuntimeClass detection
// This dramatically reduces API server load in large clusters (2000+ nodes)
type runtimeClassCache struct {
	mu          sync.RWMutex
	hasKata     bool
	lastChecked time.Time
	ttl         time.Duration
}

// Detector provides methods to detect if the current node is running Kata Containers
type Detector struct {
	nodeName         string
	clientset        kubernetes.Interface
	detectionTimeout time.Duration
	enableMetrics    bool
	rcCache          *runtimeClassCache
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

// WithRuntimeClassCacheTTL sets a custom TTL for RuntimeClass cache
// Default is 10 minutes. Longer TTL reduces API load but may delay detection of changes.
func WithRuntimeClassCacheTTL(ttl time.Duration) DetectorOption {
	return func(d *Detector) {
		d.rcCache.ttl = ttl
	}
}

// NewDetector creates a new Kata runtime detector with default configuration.
// Returns an error if the node name is invalid according to DNS-1123 subdomain rules.
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
		rcCache: &runtimeClassCache{
			ttl: DefaultRuntimeClassCacheTTL,
		},
	}

	// Apply options
	for _, opt := range opts {
		opt(d)
	}

	return d, nil
}

// NewDetectorWithTimeout creates a new Kata runtime detector with custom timeout.
// Deprecated: Use NewDetector with WithTimeout option instead.
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

// IsKataEnabled determines if Kata Containers runtime is enabled on the current node.
// It uses authoritative Kubernetes API detection methods with concurrent execution:
// 1. Kubernetes node status check (ContainerRuntimeVersion, labels, annotations)
// 2. RuntimeClass detection (checks for kata RuntimeClass handlers)
//
// Note: Filesystem detection is intentionally NOT used as filesystem artifacts
// (binaries, directories) can persist after Kata is disabled, causing false positives.
// Only API-based detection reflects the current, active runtime configuration.
//
// Returns a DetectionResult with detailed information about the detection attempt,
// including which methods were tried and any errors encountered.
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
		AttemptedMethods: make([]DetectionMethod, 0, 2),
		Errors:           make([]error, 0, 2),
	}

	slog.Info("Detecting Kata Containers runtime via Kubernetes API", "node", d.nodeName)

	// Validate prerequisites
	if err := d.validateClientset(); err != nil {
		return nil, err
	}

	// Fetch node once for both detection methods
	node, err := d.getNode(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Run concurrent detection methods
	if err := d.runConcurrentDetection(ctx, cancel, node, result); err != nil {
		return nil, err
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

// runConcurrentDetection runs detection methods concurrently with early exit
func (d *Detector) runConcurrentDetection(
	ctx context.Context,
	cancel context.CancelFunc,
	node *corev1.Node,
	result *DetectionResult,
) error {
	g, gctx := errgroup.WithContext(ctx)
	resultChan := make(chan DetectionMethod, 2)

	// Launch detection goroutines
	d.launchNodeMetadataDetection(g, gctx, node, result, resultChan)
	d.launchRuntimeClassDetection(g, gctx, result, resultChan)

	// Wait for results
	go func() {
		_ = g.Wait()

		close(resultChan)
	}()

	return d.waitForDetectionResult(ctx, cancel, resultChan, result)
}

// launchNodeMetadataDetection launches the node metadata detection goroutine
func (d *Detector) launchNodeMetadataDetection(
	g *errgroup.Group,
	gctx context.Context,
	node *corev1.Node,
	result *DetectionResult,
	resultChan chan DetectionMethod,
) {
	g.Go(func() error {
		method := DetectionMethodKubernetesAPI

		result.mu.Lock()
		result.AttemptedMethods = append(result.AttemptedMethods, method)
		result.mu.Unlock()

		start := time.Now()
		isKata := d.checkNodeMetadata(node)
		duration := time.Since(start)

		if d.enableMetrics {
			detectionAttempts.WithLabelValues(d.nodeName, string(method), "true").Inc()
			detectionDuration.WithLabelValues(
				d.nodeName,
				string(method),
				fmt.Sprintf("%t", isKata),
			).Observe(duration.Seconds())
		}

		if isKata {
			select {
			case resultChan <- method:
				slog.Info("Kata Containers detected via Kubernetes API", "node", d.nodeName)
			case <-gctx.Done():
			}
		}

		return nil
	})
}

// launchRuntimeClassDetection launches the RuntimeClass detection goroutine
func (d *Detector) launchRuntimeClassDetection(
	g *errgroup.Group,
	gctx context.Context,
	result *DetectionResult,
	resultChan chan DetectionMethod,
) {
	g.Go(func() error {
		method := DetectionMethodRuntimeClass

		result.mu.Lock()
		result.AttemptedMethods = append(result.AttemptedMethods, method)
		result.mu.Unlock()

		start := time.Now()
		isKata, err := d.detectViaRuntimeClass(gctx)
		duration := time.Since(start)

		if d.enableMetrics {
			detectionAttempts.WithLabelValues(
				d.nodeName,
				string(method),
				fmt.Sprintf("%t", err == nil),
			).Inc()
			detectionDuration.WithLabelValues(
				d.nodeName,
				string(method),
				fmt.Sprintf("%t", isKata),
			).Observe(duration.Seconds())
		}

		if err != nil {
			result.mu.Lock()
			result.Errors = append(result.Errors, fmt.Errorf("runtime class detection: %w", err))
			result.mu.Unlock()

			slog.Warn("RuntimeClass-based Kata detection failed", "error", err)

			return nil
		}

		if isKata {
			select {
			case resultChan <- method:
				slog.Info("Kata Containers detected via RuntimeClass", "node", d.nodeName)
			case <-gctx.Done():
			}
		}

		return nil
	})
}

// waitForDetectionResult waits for the first positive result or timeout
func (d *Detector) waitForDetectionResult(
	ctx context.Context,
	cancel context.CancelFunc,
	resultChan chan DetectionMethod,
	result *DetectionResult,
) error {
	var method DetectionMethod

	select {
	case m := <-resultChan:
		if m != "" {
			method = m

			result.IsKata = true

			result.Method = method

			cancel()
		}
	case <-ctx.Done():
		if d.enableMetrics {
			detectionResults.WithLabelValues(d.nodeName, "timeout").Inc()
		}

		return ctx.Err()
	}

	// Drain remaining results
	for range resultChan {
	}

	return nil
}

// recordDetectionResult logs and records metrics for the final detection result
func (d *Detector) recordDetectionResult(result *DetectionResult) {
	if d.enableMetrics {
		detectionResults.WithLabelValues(d.nodeName, fmt.Sprintf("%t", result.IsKata)).Inc()
	}

	if !result.IsKata {
		slog.Info("Kata Containers not detected, using standard runtime",
			"node", d.nodeName,
			"attempted_methods", result.AttemptedMethods,
			"errors", len(result.Errors))
	}
}

// getNode retrieves the node object using direct API call with retry
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

// checkNodeMetadata examines node metadata for Kata indicators
// This replaces detectViaKubernetesAPI and accepts a pre-fetched node
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

// checkRuntimeVersion examines the container runtime version for Kata indicators
func (d *Detector) checkRuntimeVersion(node *corev1.Node) bool {
	runtime := strings.ToLower(node.Status.NodeInfo.ContainerRuntimeVersion)
	if strings.Contains(runtime, "kata") {
		slog.Debug("Kata detected in container runtime version", "runtime", runtime)
		return true
	}

	return false
}

// checkNodeLabels looks for Kata-related node labels
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

		// Check if label value indicates enabled (could be "true", "enabled", "1", etc.)
		lowerValue := strings.ToLower(value)

		if lowerValue == "true" || lowerValue == "enabled" || lowerValue == "1" || lowerValue == "yes" {
			slog.Debug("Kata detected via node label", "label", label, "value", value)
			return true
		}
	}

	return false
}

// checkNodeAnnotations examines node annotations for Kata configuration
func (d *Detector) checkNodeAnnotations(node *corev1.Node) bool {
	kataAnnotations := []string{
		"kata-runtime.io/enabled",
		"io.katacontainers.config",
	}

	for _, annotation := range kataAnnotations {
		if value, exists := node.Annotations[annotation]; exists && value != "" {
			slog.Debug("Kata detected via node annotation", "annotation", annotation)
			return true
		}
	}

	return false
}

// detectViaRuntimeClass checks RuntimeClass resources for Kata runtime handlers
// Uses TTL-based caching to minimize API server load in large clusters (2000+ nodes)
func (d *Detector) detectViaRuntimeClass(ctx context.Context) (bool, error) {
	// Check cache first
	d.rcCache.mu.RLock()
	if time.Since(d.rcCache.lastChecked) < d.rcCache.ttl {
		result := d.rcCache.hasKata
		d.rcCache.mu.RUnlock()
		slog.Debug(
			"Using cached RuntimeClass detection result",
			"hasKata", result,
			"age", time.Since(d.rcCache.lastChecked),
		)

		return result, nil
	}
	d.rcCache.mu.RUnlock()

	// Direct API call
	rcList, err := d.clientset.NodeV1().RuntimeClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list RuntimeClasses: %w", err)
	}

	// Check for Kata runtime handlers
	hasKata := false

	for _, rc := range rcList.Items {
		handler := strings.ToLower(rc.Handler)

		if strings.Contains(handler, "kata") {
			slog.Debug("Kata RuntimeClass detected", "name", rc.Name, "handler", rc.Handler)

			hasKata = true

			break
		}
	}

	// Update cache
	d.rcCache.mu.Lock()
	d.rcCache.hasKata = hasKata
	d.rcCache.lastChecked = time.Now()
	d.rcCache.mu.Unlock()

	return hasKata, nil
}
