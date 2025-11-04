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

# Variables
TARGET_REG_URI="${1:-}"
IMAGE_LIST_FILE="${2:-versions.txt}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}ℹ️  $*${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $*${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $*${NC}"
}

log_error() {
    echo -e "${RED}❌ $*${NC}"
}

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Validate prerequisites
command_exists crane || {
    log_error "crane is not installed. Please install crane to proceed."
    exit 1
}

# Validate arguments
if [ -z "$TARGET_REG_URI" ]; then
    log_error "Usage: $0 <target-registry-uri> [image-list-file]"
    log_error "Example: $0 us-docker.pkg.dev/my-project/my-repo versions.txt"
    exit 1
}

if [ ! -f "$IMAGE_LIST_FILE" ]; then
    log_error "Image list file not found: $IMAGE_LIST_FILE"
    exit 1
}

# Info
log_info "Source image list: $IMAGE_LIST_FILE"
log_info "Target registry URI: $TARGET_REG_URI"
log_info "Reading images from $IMAGE_LIST_FILE..."

# Count total images (excluding empty lines and comments)
TOTAL_IMAGES=$(grep -v '^#' "$IMAGE_LIST_FILE" | grep -v '^[[:space:]]*$' | wc -l | tr -d '[:space:]')
log_info "Found $TOTAL_IMAGES images to copy"

# Counters
SUCCESS_COUNT=0
FAILURE_COUNT=0
SKIPPED_COUNT=0

# Copy single image function
copy_image() {
    local src_image_uri=$1
    local image_num=$2
    
    log_info "[$image_num/$TOTAL_IMAGES] Processing: $src_image_uri"
    
    # Extract image name and tag from URI
    # Format: registry/org/image:tag
    local image_base=$(echo "$src_image_uri" | sed -E 's|^(.*/)([^/]+):(.*)$|\2|')
    local image_tag=$(echo "$src_image_uri" | sed -E 's|^.*:(.*)$|\1|')
    
    # Build target URI
    local target_uri="$TARGET_REG_URI/$image_base:$image_tag"
    
    log_info "  Source: $src_image_uri"
    log_info "  Target: $target_uri"
    
    # Get source digest
    local src_digest
    if ! src_digest=$(crane digest "$src_image_uri" 2>&1); then
        log_error "  Failed to get digest for $src_image_uri: $src_digest"
        return 1
    fi
    
    log_info "  Source digest: $src_digest"
    
    # Check if image already exists at target with same digest
    local target_digest
    if target_digest=$(crane digest "$target_uri" 2>/dev/null); then
        if [ "$target_digest" = "$src_digest" ]; then
            log_warning "  Image already exists at target with same digest, skipping"
            return 2
        else
            log_info "  Image exists but digest differs, will overwrite"
        fi
    fi
    
    # Copy image
    log_info "  Copying image..."
    if ! crane copy "$src_image_uri" "$target_uri"; then
        log_error "  Failed to copy image"
        return 1
    fi
    
    # Verify digest after copy
    local new_digest
    if ! new_digest=$(crane digest "$target_uri" 2>&1); then
        log_error "  Failed to verify target digest: $new_digest"
        return 1
    fi
    
    if [ "$new_digest" != "$src_digest" ]; then
        log_error "  Digest mismatch! Source: $src_digest, Target: $new_digest"
        return 1
    fi
    
    log_success "  Successfully copied and verified: $target_uri"
    return 0
}

# Process each image in the list
IMAGE_NUM=0
while IFS= read -r src_image_uri; do
    # Skip empty lines and comments
    [[ -z "$src_image_uri" || "$src_image_uri" =~ ^[[:space:]]*# ]] && continue
    
    IMAGE_NUM=$((IMAGE_NUM + 1))
    
    if copy_image "$src_image_uri" "$IMAGE_NUM"; then
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
    elif [ $? -eq 2 ]; then
        SKIPPED_COUNT=$((SKIPPED_COUNT + 1))
    else
        FAILURE_COUNT=$((FAILURE_COUNT + 1))
        log_warning "Continuing with next image..."
    fi
    
    echo ""  # Blank line between images
done < "$IMAGE_LIST_FILE"

# Summary
echo "=================================================="
log_info "Image Copy Summary"
echo "=================================================="
log_success "Successfully copied: $SUCCESS_COUNT"
log_warning "Skipped (already exist): $SKIPPED_COUNT"
if [ $FAILURE_COUNT -gt 0 ]; then
    log_error "Failed: $FAILURE_COUNT"
else
    log_info "Failed: $FAILURE_COUNT"
fi
log_info "Total processed: $TOTAL_IMAGES"
echo "=================================================="

# Exit with error if any failures
if [ $FAILURE_COUNT -gt 0 ]; then
    exit 1
fi

exit 0
