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

# Create primary VPC for the cluster/system pool
gcloud compute networks describe "$PRIMARY_NET" >/dev/null 2>&1 || \
gcloud compute networks create "$PRIMARY_NET" \
  --subnet-mode=custom \
  --bgp-routing-mode=regional

# Create primary subnet + alias IP secondary ranges (pods/services)
gcloud compute networks subnets describe "$PRIMARY_SUBNET" --region "$REGION" >/dev/null 2>&1 || \
gcloud compute networks subnets create "$PRIMARY_SUBNET" \
  --network="$PRIMARY_NET" \
  --region="$REGION" \
  --range="$PRIMARY_CIDR" \
  --secondary-range="pods=${POD_CIDR},services=${SVC_CIDR}" \
  --enable-private-ip-google-access

# Create GPU NIC networks and subnets
for i in "${!GPU_NICS[@]}"; do
  NET="${GPU_NICS[$i]}"
  CIDR="${GPU_NIC_CIDRS[$i]}"

  gcloud compute networks describe "$NET" >/dev/null 2>&1 || \
  gcloud compute networks create "$NET" \
    --subnet-mode=custom \
    --bgp-routing-mode=regional

  gcloud compute networks subnets describe "$NET" --region "$REGION" >/dev/null 2>&1 || \
  gcloud compute networks subnets create "$NET" \
    --network="$NET" \
    --region="$REGION" \
    --range="$CIDR" \
    --enable-private-ip-google-access
done

echo "✅ Network creation complete!"
