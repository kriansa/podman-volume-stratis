#!/bin/bash
# Auto-detect version from git tags for dev builds
# If no tags exist, return 0.0.1-dev
# Otherwise, get latest tag, increment patch, append -dev

set -euo pipefail

latest_tag=$(git describe --tags --abbrev=0 2>/dev/null || true)

if [ -z "$latest_tag" ]; then
    echo "0.0.1-dev"
    exit 0
fi

# Remove leading 'v' if present
version="${latest_tag#v}"

# Parse semver (major.minor.patch)
IFS='.' read -r major minor patch <<< "$version"

# Handle case where patch might have additional suffix (e.g., 1.2.3-rc1)
patch="${patch%%-*}"

# Increment patch and add -dev
echo "${major}.${minor}.$((patch + 1))-dev"
