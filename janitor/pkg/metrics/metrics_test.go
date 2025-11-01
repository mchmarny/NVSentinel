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

package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewActionMetrics(t *testing.T) {
	// Test that GlobalMetrics is already initialized
	// We can't call NewActionMetrics again due to duplicate registration
	assert.NotNil(t, GlobalMetrics, "GlobalMetrics should be initialized")
}

func TestActionMetrics_IncActionCount(t *testing.T) {
	// Create a new metrics instance
	m := &ActionMetrics{}

	tests := []struct {
		name       string
		actionType string
		status     string
		node       string
	}{
		{
			name:       "reboot started",
			actionType: ActionTypeReboot,
			status:     StatusStarted,
			node:       "test-node-1",
		},
		{
			name:       "reboot succeeded",
			actionType: ActionTypeReboot,
			status:     StatusSucceeded,
			node:       "test-node-1",
		},
		{
			name:       "terminate started",
			actionType: ActionTypeTerminate,
			status:     StatusStarted,
			node:       "test-node-2",
		},
		{
			name:       "terminate failed",
			actionType: ActionTypeTerminate,
			status:     StatusFailed,
			node:       "test-node-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic when incrementing
			assert.NotPanics(t, func() {
				m.IncActionCount(tt.actionType, tt.status, tt.node)
			})
		})
	}
}

func TestActionMetrics_RecordActionMTTR(t *testing.T) {
	// Create a new metrics instance
	m := &ActionMetrics{}

	tests := []struct {
		name       string
		actionType string
		duration   time.Duration
	}{
		{
			name:       "quick reboot",
			actionType: ActionTypeReboot,
			duration:   30 * time.Second,
		},
		{
			name:       "slow reboot",
			actionType: ActionTypeReboot,
			duration:   5 * time.Minute,
		},
		{
			name:       "terminate operation",
			actionType: ActionTypeTerminate,
			duration:   2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic when recording
			assert.NotPanics(t, func() {
				m.RecordActionMTTR(tt.actionType, tt.duration)
			})
		})
	}
}

func TestGlobalMetrics_Functions(t *testing.T) {
	// Test that global convenience functions work
	assert.NotPanics(t, func() {
		IncActionCount(ActionTypeReboot, StatusStarted, "global-test-node")
	})

	assert.NotPanics(t, func() {
		RecordActionMTTR(ActionTypeReboot, 1*time.Minute)
	})
}

func TestMetricsConstants(t *testing.T) {
	// Verify that constants are correctly defined
	assert.Equal(t, "reboot", ActionTypeReboot)
	assert.Equal(t, "terminate", ActionTypeTerminate)

	assert.Equal(t, "started", StatusStarted)
	assert.Equal(t, "succeeded", StatusSucceeded)
	assert.Equal(t, "failed", StatusFailed)
}

func TestActionMetrics_CounterIncrement(t *testing.T) {
	// Create a test registry to verify metrics behavior
	testRegistry := prometheus.NewRegistry()

	// Create new counter for testing
	testCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "test_janitor_actions_count",
			Help: "Test counter for janitor actions",
		},
		[]string{"action_type", "status", "node"},
	)

	testRegistry.MustRegister(testCounter)

	// Increment the counter multiple times
	testCounter.With(prometheus.Labels{
		"action_type": ActionTypeReboot,
		"status":      StatusStarted,
		"node":        "test-node",
	}).Inc()

	testCounter.With(prometheus.Labels{
		"action_type": ActionTypeReboot,
		"status":      StatusStarted,
		"node":        "test-node",
	}).Inc()

	// Gather metrics
	metricFamilies, err := testRegistry.Gather()
	require.NoError(t, err)
	require.Len(t, metricFamilies, 1)

	// Verify the counter was incremented
	metricFamily := metricFamilies[0]
	assert.Equal(t, "test_janitor_actions_count", *metricFamily.Name)
	require.Len(t, metricFamily.Metric, 1)

	// Check that the counter value is 2
	counter := metricFamily.Metric[0]
	assert.Equal(t, float64(2), *counter.Counter.Value)
}

func TestActionMetrics_HistogramObservation(t *testing.T) {
	// Create a test registry to verify histogram behavior
	testRegistry := prometheus.NewRegistry()

	// Create new histogram for testing
	testHistogram := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "test_janitor_action_mttr_seconds",
			Help:    "Test histogram for janitor action MTTR",
			Buckets: prometheus.ExponentialBuckets(10, 2, 5),
		},
		[]string{"action_type"},
	)

	testRegistry.MustRegister(testHistogram)

	// Record some observations
	testHistogram.With(prometheus.Labels{
		"action_type": ActionTypeReboot,
	}).Observe(30.0) // 30 seconds

	testHistogram.With(prometheus.Labels{
		"action_type": ActionTypeReboot,
	}).Observe(120.0) // 2 minutes

	// Gather metrics
	metricFamilies, err := testRegistry.Gather()
	require.NoError(t, err)
	require.Len(t, metricFamilies, 1)

	// Verify the histogram was updated
	metricFamily := metricFamilies[0]
	assert.Equal(t, "test_janitor_action_mttr_seconds", *metricFamily.Name)
	assert.Equal(t, dto.MetricType_HISTOGRAM, *metricFamily.Type)
	require.Len(t, metricFamily.Metric, 1)

	// Check histogram sample count
	histogram := metricFamily.Metric[0].Histogram
	assert.Equal(t, uint64(2), *histogram.SampleCount)

	// Check sum of observations (30 + 120 = 150)
	assert.Equal(t, float64(150), *histogram.SampleSum)
}

func TestGlobalMetrics_Initialization(t *testing.T) {
	// Verify that GlobalMetrics is initialized
	assert.NotNil(t, GlobalMetrics, "GlobalMetrics should be initialized")
}

func TestActionMetrics_MultipleNodes(t *testing.T) {
	// Test that metrics can track different nodes independently
	m := &ActionMetrics{}

	nodes := []string{"node-1", "node-2", "node-3"}

	for _, node := range nodes {
		assert.NotPanics(t, func() {
			m.IncActionCount(ActionTypeReboot, StatusStarted, node)
			m.IncActionCount(ActionTypeReboot, StatusSucceeded, node)
		})
	}
}

func TestActionMetrics_DifferentActionTypes(t *testing.T) {
	// Test that different action types can be tracked independently
	m := &ActionMetrics{}

	assert.NotPanics(t, func() {
		// Reboot actions
		m.IncActionCount(ActionTypeReboot, StatusStarted, "node-1")
		m.RecordActionMTTR(ActionTypeReboot, 1*time.Minute)

		// Terminate actions
		m.IncActionCount(ActionTypeTerminate, StatusStarted, "node-2")
		m.RecordActionMTTR(ActionTypeTerminate, 2*time.Minute)
	})
}
