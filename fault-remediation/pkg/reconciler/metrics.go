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

package reconciler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// event processing metrics
	totalEventsReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "fault_remediation_events_received_total",
			Help: "Total number of events received from the watcher.",
		},
	)
	totalEventsSuccessfullyProcessed = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "fault_remediation_events_successfully_processed_total",
			Help: "Total number of events successfully processed.",
		},
	)
	totalEventProcessingError = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fault_remediation_processing_errors_total",
			Help: "Total number of errors encountered during event processing.",
		},
		[]string{"error_type", "node_name"},
	)
	totalUnsupportedRemediationActions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fault_remediation_unsupported_actions_total",
			Help: "Total number of health events with currently unsupported remediation actions.",
		},
		[]string{"action", "node_name"},
	)

	// log collection job metrics
	logCollectorJobs = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fault_remediation_log_collector_jobs_total",
			Help: "Total number of log collector jobs.",
		},
		[]string{"node_name", "status"},
	)
	logCollectorJobDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "fault_remediation_log_collector_job_duration_seconds",
			Help:    "Duration of log collector jobs in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"node_name", "status"},
	)
)
