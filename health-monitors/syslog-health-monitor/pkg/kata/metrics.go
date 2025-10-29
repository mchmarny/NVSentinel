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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	detectionDuration *prometheus.HistogramVec
	detectionAttempts *prometheus.CounterVec
	detectionResults  *prometheus.CounterVec
	metricsEnabled    bool
)

// InitMetrics initializes and registers Kata detection metrics.
// Must be called before creating detectors if metrics are desired.
// If not called, metric recording will be safely skipped (no-op).
func InitMetrics(reg prometheus.Registerer, enabled bool) {
	metricsEnabled = enabled
	if !enabled || reg == nil {
		return
	}

	detectionDuration = promauto.With(reg).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kata_detection_duration_seconds",
			Help:    "Duration of Kata detection operations",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"node", "method", "result"},
	)

	detectionAttempts = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Name: "kata_detection_attempts_total",
			Help: "Total Kata detection attempts by method",
		},
		[]string{"node", "method", "success"},
	)

	detectionResults = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Name: "kata_detection_results_total",
			Help: "Total Kata detection results",
		},
		[]string{"node", "detected"},
	)
}
