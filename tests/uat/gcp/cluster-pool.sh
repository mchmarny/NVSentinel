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

DIR="$(dirname "$0")"
. "${DIR}/cluster-env.sh"

# Get cluster version 
echo "Cluster version:"
gcloud container clusters describe "$CLUSTER_NAME" \
    --region="$REGION" \
    --format="value(currentMasterVersion)"

# Add GPU node pool if specified
if [[ "$GPU_NODE_COUNT" -gt 0 ]]; then
    echo "Adding GPU node pool..."
    
    # Base command for GPU instances
    # Note: A3/A4 instances have 8x H100-80GB GPUs pre-attached
    # DO NOT use --accelerator flag - GPUs are part of the machine type
    # --node-locations specifies the zone(s) where nodes will be created (must match capacity reservation zone)
    CMD=(
        gcloud container node-pools create gpu-pool
            --cluster="$CLUSTER_NAME"
            --region="$REGION"
            --node-locations="$GPU_NODE_ZONE"
            --machine-type="$GPU_NODE_TYPE"
            --image-type="COS_CONTAINERD"
            --num-nodes="$GPU_NODE_COUNT"
            --scopes="https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/dataaccessauditlogging"
            --workload-metadata=GKE_METADATA
            --enable-gvnic
            --node-taints="dedicated=user-workload:NoExecute"
            --node-labels="nodeGroup=customer-gpu,dedicated=user-workload,gke-no-default-nvidia-gpu-device-plugin=true"
            --tags="customer-gpu,customer-node"
            --shielded-secure-boot
            --shielded-integrity-monitoring
    )
    
    # Add capacity reservation only if specified
    if [[ -n "$GPU_NODE_CAPACITY_RESERVATION" ]]; then
        CMD+=(
            --reservation-affinity=specific
            --reservation="$GPU_NODE_CAPACITY_RESERVATION"
        )
    fi
    
    # Execute the command
    "${CMD[@]}"
fi
