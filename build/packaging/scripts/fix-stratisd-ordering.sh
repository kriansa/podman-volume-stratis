#!/bin/bash
# Workaround for https://github.com/stratis-storage/stratisd/issues/3968
# stratisd.service has After=multi-user.target which creates an ordering cycle
# for any service in multi-user.target that depends on stratisd.
#
# We use a full unit override at /etc/systemd/system/stratisd.service instead of
# a drop-in because systemd drop-ins cannot remove After= directives — ordering
# dependencies can only be added, not reset, via drop-in files.
#
# This script creates the full unit override with the problematic directives removed.

set -euo pipefail

SOURCE=/usr/lib/systemd/system/stratisd.service
DEST=/etc/systemd/system/stratisd.service

# If stratisd is not installed, clean up any existing override
if [[ ! -f "$SOURCE" ]]; then
    rm -f "$DEST"
    systemctl daemon-reload
    exit 0
fi

# Check if the fix is still needed
if ! grep -q '^After=multi-user.target' "$SOURCE" && \
   ! grep -q '^DefaultDependencies=no' "$SOURCE"; then
    # Upstream fix has landed — remove our override if present
    if [[ -f "$DEST" ]]; then
        rm -f "$DEST"
        systemctl daemon-reload
    fi
    exit 0
fi

# Create override: copy and patch
cp "$SOURCE" "$DEST"
sed -i '/^DefaultDependencies=no$/d' "$DEST"
sed -i '/^After=multi-user.target$/d' "$DEST"
systemctl daemon-reload
