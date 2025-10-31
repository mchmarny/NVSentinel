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
# verify-image-attestations.sh
#
# Validates SBOM attestations for all NVSentinel container images built with a specific tag.
# This script checks both Ko-built images (Go services) and Docker-built images (Python services).
#
# Usage:
#   ./scripts/verify-image-attestations.sh <tag>
#   ./scripts/verify-image-attestations.sh v1.2.3
#   ./scripts/verify-image-attestations.sh 3b37e68
#
# Requirements:
#   - crane (for manifest inspection)
#   - cosign (for attestation verification)
#   - jq (for JSON parsing)

set -euo pipefail

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REGISTRY="${REGISTRY:-ghcr.io}"
ORG="${ORG:-nvidia}"
REPO_OWNER="${REPO_OWNER:-nvidia}"
REPO_NAME="${REPO_NAME:-nvsentinel}"

# Image lists
KO_IMAGES=(
    "nvsentinel/fault-quarantine-module"
    "nvsentinel/fault-remediation-module"
    "nvsentinel/health-events-analyzer"
    "nvsentinel/csp-health-monitor"
    "nvsentinel/maintenance-notifier"
    "nvsentinel/labeler"
    "nvsentinel/node-drainer"
    "nvsentinel/janitor"
    "nvsentinel/platform-connectors"
)

DOCKER_IMAGES=(
    "nvsentinel/gpu-health-monitor:dcgm-3.x"
    "nvsentinel/gpu-health-monitor:dcgm-4.x"
    "nvsentinel/syslog-health-monitor"
    "nvsentinel/log-collector"
    "nvsentinel/file-server-cleanup"
)

# Counters
TOTAL_IMAGES=0
PASSED_IMAGES=0
FAILED_IMAGES=0
SKIPPED_IMAGES=0

# Usage
usage() {
    cat <<EOF
Usage: $0 <tag>

Validates SBOM attestations for all NVSentinel container images.

Arguments:
  tag         Image tag to verify (e.g., v1.2.3, 3b37e68)

Environment Variables:
  REGISTRY    Container registry (default: ghcr.io)
  ORG         Organization name (default: nvidia)
  REPO_OWNER  GitHub repo owner (default: nvidia)
  REPO_NAME   GitHub repo name (default: nvsentinel)

Examples:
  $0 v1.2.3
  $0 3b37e68
  REGISTRY=my-registry.io ORG=myorg $0 main-abc1234

EOF
    exit 1
}

# Check required tools
check_requirements() {
    local missing_tools=()
    
    for tool in crane cosign jq; do
        if ! command -v "$tool" &> /dev/null; then
            missing_tools+=("$tool")
        fi
    done
    
    if [ ${#missing_tools[@]} -ne 0 ]; then
        echo -e "${RED}Error: Missing required tools: ${missing_tools[*]}${NC}"
        echo "Please install them before running this script."
        exit 1
    fi
}

# Extract platform digests from multi-platform image
get_platform_digests() {
    local image_ref="$1"
    local manifest
    
    manifest=$(crane manifest "$image_ref" 2>/dev/null || echo "")
    
    if [ -z "$manifest" ]; then
        return 1
    fi
    
    # Check if it's a multi-platform index
    local media_type
    media_type=$(echo "$manifest" | jq -r '.mediaType')
    
    if [[ "$media_type" == "application/vnd.oci.image.index.v1+json" ]] || \
       [[ "$media_type" == "application/vnd.docker.distribution.manifest.list.v2+json" ]]; then
        # Extract individual platform digests
        echo "$manifest" | jq -r '.manifests[] | select(.platform.architecture != "unknown") | .digest'
    else
        # Single platform image - get its digest
        crane digest "$image_ref" 2>/dev/null || echo ""
    fi
}

# Verify GitHub attestation
verify_github_attestation() {
    local image_ref="$1"
    
    if gh attestation verify "oci://${image_ref}" --owner "$REPO_OWNER" &>/dev/null; then
        return 0
    else
        return 1
    fi
}

# Verify Cosign SBOM attestation
verify_cosign_attestation() {
    local image_ref="$1"
    
    # Check if SBOM tag exists
    if cosign tree "$image_ref" 2>&1 | grep -q "SBOM"; then
        return 0
    else
        return 1
    fi
}

# Verify attestations for a single image digest
verify_image_digest() {
    local image_name="$1"
    local digest="$2"
    local image_ref="${REGISTRY}/${ORG}/${image_name}@${digest}"
    
    echo -e "${BLUE}  Platform: ${digest:7:12}...${NC}"
    
    local github_ok=false
    local cosign_ok=false
    
    # Verify GitHub attestation
    if verify_github_attestation "$image_ref"; then
        echo -e "${GREEN}    ✓ GitHub build provenance attestation${NC}"
        github_ok=true
    else
        echo -e "${YELLOW}    ⚠ GitHub build provenance attestation not found${NC}"
    fi
    
    # Verify Cosign SBOM attestation
    if verify_cosign_attestation "$image_ref"; then
        echo -e "${GREEN}    ✓ Cosign SBOM attestation${NC}"
        cosign_ok=true
    else
        echo -e "${RED}    ✗ Cosign SBOM attestation not found${NC}"
    fi
    
    if $github_ok && $cosign_ok; then
        return 0
    else
        return 1
    fi
}

# Verify attestations for a single image
verify_image() {
    local image_name="$1"
    local tag="$2"
    local image_ref="${REGISTRY}/${ORG}/${image_name}:${tag}"
    
    echo -e "\n${BLUE}Verifying: ${image_name}:${tag}${NC}"
    TOTAL_IMAGES=$((TOTAL_IMAGES + 1))
    
    # Check if image exists
    if ! crane manifest "$image_ref" &>/dev/null; then
        echo -e "${YELLOW}  ⊘ Image not found, skipping${NC}"
        SKIPPED_IMAGES=$((SKIPPED_IMAGES + 1))
        return
    fi
    
    # Get platform digests
    local digests
    digests=$(get_platform_digests "$image_ref")
    
    if [ -z "$digests" ]; then
        echo -e "${RED}  ✗ Failed to get image digests${NC}"
        FAILED_IMAGES=$((FAILED_IMAGES + 1))
        return
    fi
    
    # Verify each platform
    local all_passed=true
    while IFS= read -r digest; do
        if ! verify_image_digest "$image_name" "$digest"; then
            all_passed=false
        fi
    done <<< "$digests"
    
    if $all_passed; then
        echo -e "${GREEN}  ✓ All attestations verified${NC}"
        PASSED_IMAGES=$((PASSED_IMAGES + 1))
    else
        echo -e "${RED}  ✗ Some attestations missing${NC}"
        FAILED_IMAGES=$((FAILED_IMAGES + 1))
    fi
}

# Main function
main() {
    if [ $# -ne 1 ]; then
        usage
    fi
    
    local tag="$1"
    
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  NVSentinel Image Attestation Verification${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo -e "Registry: ${REGISTRY}"
    echo -e "Organization: ${ORG}"
    echo -e "Tag: ${tag}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    
    # Check requirements
    check_requirements
    
    # Verify Ko-built images
    echo -e "\n${BLUE}═══ Ko-built Images (Go services) ═══${NC}"
    for image in "${KO_IMAGES[@]}"; do
        verify_image "$image" "$tag"
    done
    
    # Verify Docker-built images
    echo -e "\n${BLUE}═══ Docker-built Images ═══${NC}"
    for image_spec in "${DOCKER_IMAGES[@]}"; do
        # Handle images with tag suffixes (e.g., gpu-health-monitor:dcgm-3.x)
        if [[ "$image_spec" == *":"* ]]; then
            image_base="${image_spec%:*}"
            suffix="${image_spec#*:}"
            full_tag="${tag}-${suffix}"
        else
            image_base="$image_spec"
            full_tag="$tag"
        fi
        verify_image "$image_base" "$full_tag"
    done
    
    # Print summary
    echo -e "\n${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  Verification Summary${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo -e "Total images checked: ${TOTAL_IMAGES}"
    echo -e "${GREEN}Passed: ${PASSED_IMAGES}${NC}"
    echo -e "${RED}Failed: ${FAILED_IMAGES}${NC}"
    echo -e "${YELLOW}Skipped: ${SKIPPED_IMAGES}${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    
    if [ $FAILED_IMAGES -gt 0 ]; then
        echo -e "\n${RED}Some images are missing attestations!${NC}"
        exit 1
    elif [ $PASSED_IMAGES -eq 0 ]; then
        echo -e "\n${YELLOW}No images were successfully verified.${NC}"
        exit 1
    else
        echo -e "\n${GREEN}All images have valid attestations!${NC}"
        exit 0
    fi
}

main "$@"
