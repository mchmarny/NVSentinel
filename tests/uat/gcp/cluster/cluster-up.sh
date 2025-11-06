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

# Assumptions:
# - gcloud is installed and configured
# - OIDC configured (see https://github.com/mchmarny/oidc-for-gcp-using-terraform)

# Create regional cluster
echo "Creating GKE cluster..."
gcloud container clusters create "$CLUSTER_NAME" \
    --cluster-version "$CLUSTER_VERSION" \
    --scopes=cloud-platform \
    --disk-size="200" \
    --disk-type="pd-standard" \
    --enable-image-streaming \
    --enable-ip-alias \
    --enable-shielded-nodes \
    --enable-autorepair \
    --enable-network-policy \
    --image-type="COS_CONTAINERD" \
    --labels=source=github,environment=validation \
    --logging=SYSTEM,WORKLOAD \
    --machine-type="$SYSTEM_NODE_TYPE" \
    --monitoring=SYSTEM \
    --num-nodes="$SYSTEM_NODE_COUNT" \
    --region="$REGION" \
    --release-channel="$CLUSTER_CHANNEL" \
    --workload-metadata="GKE_METADATA" \
    --workload-pool="${PROJECT_ID}.svc.id.goog" \
    --addons=HttpLoadBalancing,HorizontalPodAutoscaling,GcePersistentDiskCsiDriver \
    --network="$PRIMARY_NET" \
    --subnetwork="$PRIMARY_SUBNET" \
    --cluster-secondary-range-name=pods \
    --services-secondary-range-name=services

# Get cluster version 
echo "Cluster version:"
gcloud container clusters describe "$CLUSTER_NAME" \
    --region="$REGION" \
    --format="value(currentMasterVersion)"

# Install Auth Plugin
gcloud components install kubectl --quiet
gcloud components install gke-gcloud-auth-plugin --quiet

echo "✅ Cluster creation complete!"
