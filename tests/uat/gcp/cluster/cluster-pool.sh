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

# Add GPU node pool if specified
if [[ "$GPU_NODE_COUNT" -gt 0 ]]; then
    echo "Adding GPU node pool..."
    
    # Base command for GPU instances
    CMD=(
        gcloud container node-pools create gpu-pool
            --accelerator="type=nvidia-h100-mega-80gb,count=8,gpu-driver-version=DISABLED"            
            "--additional-node-network=network=${GPU_NICS[0]},subnetwork=${GPU_NICS[0]}"
            "--additional-node-network=network=${GPU_NICS[1]},subnetwork=${GPU_NICS[1]}"
            "--additional-node-network=network=${GPU_NICS[2]},subnetwork=${GPU_NICS[2]}"
            "--additional-node-network=network=${GPU_NICS[3]},subnetwork=${GPU_NICS[3]}"
            "--additional-node-network=network=${GPU_NICS[4]},subnetwork=${GPU_NICS[4]}"
            "--additional-node-network=network=${GPU_NICS[5]},subnetwork=${GPU_NICS[5]}"
            "--additional-node-network=network=${GPU_NICS[6]},subnetwork=${GPU_NICS[6]}"
            "--additional-node-network=network=${GPU_NICS[7]},subnetwork=${GPU_NICS[7]}"
            --cluster="$CLUSTER_NAME"
            --disk-size=200
            --disk-type=pd-ssd
            --enable-gvnic
            --image-type="COS_CONTAINERD"
            --local-nvme-ssd-block=count=16
            --machine-type="$GPU_NODE_TYPE"
            --max-pods-per-node=110
            --metadata=disable-legacy-endpoints=true
            --node-labels="nodeGroup=customer-gpu,dedicated=user-workload,gke-no-default-nvidia-gpu-device-plugin=true,env=non-prod"
            --node-locations="$GPU_NODE_ZONE"
            --node-taints="dedicated=user-workload:NoExecute"
            --num-nodes="$GPU_NODE_COUNT"
            --placement-type=COMPACT
            --region="$REGION"
            --scopes="https://www.googleapis.com/auth/userinfo.email,https://www.googleapis.com/auth/cloud-platform"
            --service-account="$SERVICE_ACCOUNT"
            --shielded-integrity-monitoring
            --shielded-secure-boot
            --tags="customer-gpu,customer-node"
            --workload-metadata=GKE_METADATA
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
