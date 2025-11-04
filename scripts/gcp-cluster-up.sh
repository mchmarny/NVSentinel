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
. "${DIR}/gcp-cluster-env.sh"

# Assumptions:
# - gcloud is installed and configured
# - OIDC configured (see https://github.com/mchmarny/oidc-for-gcp-using-terraform)


# Check if default network exists, create if missing
echo "Checking for VPC network..."
if ! gcloud compute networks describe default --format="value(name)" >/dev/null 2>&1; then
    echo "Creating default VPC network..."
    gcloud compute networks create default --subnet-mode=auto
    echo "✅ Default network created"
fi

# Create regional cluster
echo "Creating GKE cluster..."
gcloud container clusters create "$CLUSTER_NAME" \
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
    --addons=HttpLoadBalancing,HorizontalPodAutoscaling

# Get cluster version 
echo "Cluster version:"
gcloud container clusters describe "$CLUSTER_NAME" \
    --region="$REGION" \
    --format="value(currentMasterVersion)"

# Create policy binding between service account and k8s service account (optional)
if [[ -n "${SERVICE_ACCOUNT}" ]]; then
    echo "Creating IAM policy binding for service account..."
    gcloud iam service-accounts add-iam-policy-binding "${SERVICE_ACCOUNT}" \
        --member="serviceAccount:${PROJECT_ID}.svc.id.goog[cnrm-system/cnrm-controller-manager]" \
        --role="roles/iam.workloadIdentityUser"
fi

# Get cluster credentials
echo "Getting cluster credentials..."
gcloud container clusters get-credentials "$CLUSTER_NAME" --region="$REGION"

echo "✅ Cluster creation complete!"
