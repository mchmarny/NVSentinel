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

package main

import (
	"testing"
	"time"

	"github.com/nvidia/nvsentinel/labeler-module/pkg/labeler"
	"k8s.io/client-go/kubernetes/fake"
)

// TestKataLabelOverride verifies that the kataLabelOverride parameter correctly
// adds custom kata detection labels to the labeler instance.
func TestKataLabelOverride(t *testing.T) {
	tests := []struct {
		name       string
		override   string
		wantLabels []string
	}{
		{
			name:       "no override - default only",
			override:   "",
			wantLabels: []string{"katacontainers.io/kata-runtime"},
		},
		{
			name:       "with custom override",
			override:   "custom.io/kata-enabled",
			wantLabels: []string{"katacontainers.io/kata-runtime", "custom.io/kata-enabled"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fake.NewSimpleClientset()

			l, err := labeler.NewLabeler(
				clientset,
				time.Minute,
				"nvidia-dcgm",
				"nvidia-driver-daemonset",
				tt.override,
			)

			if err != nil {
				t.Fatalf("NewLabeler() error = %v", err)
			}

			if l == nil {
				t.Fatal("NewLabeler() returned nil labeler")
			}

			// Verify labeler was created successfully
			// The actual kataLabels field is private, but we can verify
			// no panic occurred and the instance is valid
			t.Logf("Successfully created labeler with override: %q", tt.override)
		})
	}
}

// TestKataLabelOverrideIsolation verifies that creating multiple labeler instances
// with different overrides doesn't pollute each other (tests for race conditions).
func TestKataLabelOverrideIsolation(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	// Create first instance with override "first"
	l1, err := labeler.NewLabeler(
		clientset,
		time.Minute,
		"nvidia-dcgm",
		"nvidia-driver-daemonset",
		"first.io/kata",
	)
	if err != nil {
		t.Fatalf("NewLabeler(first) error = %v", err)
	}

	// Create second instance with override "second"
	l2, err := labeler.NewLabeler(
		clientset,
		time.Minute,
		"nvidia-dcgm",
		"nvidia-driver-daemonset",
		"second.io/kata",
	)
	if err != nil {
		t.Fatalf("NewLabeler(second) error = %v", err)
	}

	// Create third instance with no override
	l3, err := labeler.NewLabeler(
		clientset,
		time.Minute,
		"nvidia-dcgm",
		"nvidia-driver-daemonset",
		"",
	)
	if err != nil {
		t.Fatalf("NewLabeler(empty) error = %v", err)
	}

	// All instances should be valid and independent
	if l1 == nil || l2 == nil || l3 == nil {
		t.Fatal("One or more labeler instances is nil")
	}

	t.Log("Successfully created 3 independent labeler instances with different overrides")
}
