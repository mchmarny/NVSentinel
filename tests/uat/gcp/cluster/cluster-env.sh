#!/usr/bin/env bash
#
# Copyright (c) 2025, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

set -euo pipefail

# validation 
which gcloud >/dev/null 2>&1 || ( echo "gcloud not found" && exit 1 )

# Check gcloud is authenticated.
ACCOUNT=$(gcloud auth list --filter=status:ACTIVE --format="value(account)")
if [[ -z "${ACCOUNT}" ]]; then
  echo "Run 'gcloud auth login' to authenticate to GCP first."
  exit 1
fi;

# Check project is set
PROJECT_ID=$(gcloud config list --format 'value(core.project)')
export PROJECT_ID
if [[ -z "${PROJECT_ID}" ]]; then
  echo "Project not set. Run: gcloud config set project YOUR_PROJECT_ID"
  exit 1
fi;

# DEPLOYMENT_PREFIX must be set externally
export DEPLOYMENT_PREFIX="${DEPLOYMENT_PREFIX:-}"

# Config
export REGION="${REGION:-europe-west4}"
export CLUSTER_VERSION="${CLUSTER_VERSION:-1.33.5-gke.1162000}"
export CLUSTER_NAME="${CLUSTER_NAME:-${DEPLOYMENT_PREFIX}-nvsentinel}"
export CLUSTER_CHANNEL="${CLUSTER_CHANNEL:-regular}"
export SYSTEM_NODE_TYPE="${SYSTEM_NODE_TYPE:-e2-standard-4}"
export SYSTEM_NODE_COUNT="${SYSTEM_NODE_COUNT:-1}"
export GPU_NODE_TYPE="${GPU_NODE_TYPE:-a3-megagpu-8g}"  # A3, not A4 (org policy blocks A4)
export GPU_NODE_COUNT="${GPU_NODE_COUNT:-1}"
export GPU_NODE_ZONE="${GPU_NODE_ZONE:-${REGION}-b}"
export GPU_NODE_CAPACITY_RESERVATION="${GPU_NODE_CAPACITY_RESERVATION:-}"

# SERVICE_ACCOUNT is optional - set by workflow or provide manually
export SERVICE_ACCOUNT="${SERVICE_ACCOUNT:-}"
CURRENT_ACCOUNT=$(gcloud config get-value account)
export CURRENT_ACCOUNT

# primary network for cluster & system pool
PRIMARY_NET="${PRIMARY_NET:-net-${DEPLOYMENT_PREFIX}}"
PRIMARY_SUBNET="${PRIMARY_SUBNET:-sub-${DEPLOYMENT_PREFIX}}"

# CIDRs
PRIMARY_CIDR="${PRIMARY_CIDR:-10.0.0.0/17}"
POD_CIDR="${POD_CIDR:-192.168.128.0/17}"
SVC_CIDR="${SVC_CIDR:-192.168.0.0/20}"

# 8 extra NIC networks (one per NIC)
GPU_NICS=("n-${DEPLOYMENT_PREFIX}-gpu-nic0" \
          "n-${DEPLOYMENT_PREFIX}-gpu-nic1" \
          "n-${DEPLOYMENT_PREFIX}-gpu-nic2" \
          "n-${DEPLOYMENT_PREFIX}-gpu-nic3" \
          "n-${DEPLOYMENT_PREFIX}-gpu-nic4" \
          "n-${DEPLOYMENT_PREFIX}-gpu-nic5" \
          "n-${DEPLOYMENT_PREFIX}-gpu-nic6" \
          "n-${DEPLOYMENT_PREFIX}-gpu-nic7")

# CIDRs for NIC subnets (used by cluster-network.sh)
export GPU_NIC_CIDRS=("10.200.0.0/24" "10.200.1.0/24" "10.200.2.0/24" "10.200.3.0/24" \
                      "10.200.4.0/24" "10.200.5.0/24" "10.200.6.0/24" "10.200.7.0/24")

# Print variables
cat << EOF

Configuration:
  PROJECT_ID:           ${PROJECT_ID}
  REGION:               ${REGION}
  SERVICE_ACCOUNT:      ${SERVICE_ACCOUNT}
  CURRENT_ACCOUNT:      ${CURRENT_ACCOUNT}

  CLUSTER_NAME:         ${CLUSTER_NAME}
  CLUSTER_VERSION:      ${CLUSTER_VERSION}
  CLUSTER_CHANNEL:      ${CLUSTER_CHANNEL}

  SYSTEM_NODE_TYPE:     ${SYSTEM_NODE_TYPE}
  SYSTEM_NODE_COUNT:    ${SYSTEM_NODE_COUNT}

  GPU_NODE_TYPE:        ${GPU_NODE_TYPE}
  GPU_NODE_COUNT:       ${GPU_NODE_COUNT}

  PRIMARY_NET:         ${PRIMARY_NET}
  PRIMARY_SUBNET:      ${PRIMARY_SUBNET}
  GPU_NICS:            ${GPU_NICS[@]}

EOF
