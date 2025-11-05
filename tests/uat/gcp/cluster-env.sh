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
gcloud=$(which gcloud) || ( echo "gcloud not found" && exit 1 )

# Check gcloud is authenticated.
ACCOUNT=$(gcloud auth list --filter=status:ACTIVE --format="value(account)")
if [[ -z "${ACCOUNT}" ]]; then
  echo "Run 'gcloud auth login' to authenticate to GCP first."
  exit 1
fi;

# Check project is set
export PROJECT_ID=$(gcloud config list --format 'value(core.project)')
if [[ -z "${PROJECT_ID}" ]]; then
  echo "`gcloud config set project YOUR_PROJECT_ID` note set."
  exit 1
fi;

# Check region is set
export REGION=$(gcloud config list --format 'value(compute.region)')
if [[ -z "${REGION}" ]]; then
  echo "Warning: \`gcloud config set compute/region YOUR_REGION\` not set, using default."
  export REGION="us-central1"
fi

# If variable CLUSTER_SUFFIX is not set, default to timestamp
export CLUSTER_NAME_SUFFIX="${CLUSTER_NAME_SUFFIX:-$(date +%s)}"

# Config
export CLUSTER_VERSION="${CLUSTER_VERSION:-1.33.5-gke.1162000}"
export CLUSTER_NAME="${CLUSTER_NAME:-validation-${CLUSTER_NAME_SUFFIX}}"
export CLUSTER_CHANNEL="${CLUSTER_CHANNEL:-regular}"
export SYSTEM_NODE_TYPE="${SYSTEM_NODE_TYPE:-e2-standard-4}"
export SYSTEM_NODE_COUNT="${SYSTEM_NODE_COUNT:-3}"
export GPU_NODE_TYPE="${GPU_NODE_TYPE:-a4-highgpu-8g}"
export GPU_NODE_COUNT="${GPU_NODE_COUNT:-1}"
export GPU_NODE_ACCELERATOR="${GPU_NODE_ACCELERATOR:-type=nvidia-h100-mega-80gb,count=8}"
export GPU_NODE_CAPACITY_RESERVATION="${GPU_NODE_CAPACITY_RESERVATION:-}"

# SERVICE_ACCOUNT is optional - set by workflow or provide manually
export SERVICE_ACCOUNT="${SERVICE_ACCOUNT:-}"
export CURRENT_ACCOUNT=$(gcloud config get-value account)

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
  GPU_NODE_ACCELERATOR: ${GPU_NODE_ACCELERATOR}

EOF
