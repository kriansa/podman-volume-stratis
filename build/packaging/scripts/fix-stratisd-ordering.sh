#!/bin/bash
# Patch stratisd.service to apply two customizations:
#
# 1. Workaround for https://github.com/stratis-storage/stratisd/issues/3968 —
#    stratisd.service ships with DefaultDependencies=no + After=multi-user.target
#    which creates an ordering cycle for any service in multi-user.target that
#    depends on stratisd.
#
# 2. Restart/timeout hardening for boot reliability — upstream uses
#    Restart=on-abort (does not fire on timeout) and an effective
#    TimeoutStartSec=15s (too short during slow boots, when pool setup can
#    take >10s after device discovery).
#
# We use a full unit override at /etc/systemd/system/stratisd.service instead of
# a drop-in because systemd drop-ins cannot remove After= directives — ordering
# dependencies can only be added, not reset, via drop-in files.
#
# Every operation below is idempotent on its own — running this script any
# number of times produces the same final $DEST.

set -euo pipefail

SOURCE=/usr/lib/systemd/system/stratisd.service
DEST=/etc/systemd/system/stratisd.service

# If stratisd is not installed, clean up any existing override
if [[ ! -f "$SOURCE" ]]; then
    rm -f "$DEST"
    systemctl daemon-reload
    exit 0
fi

# Recreate the override from upstream source
cp "$SOURCE" "$DEST"

# Apply ordering-cycle workaround only if the bug is still present upstream
if grep -q '^After=multi-user.target' "$SOURCE" || \
   grep -q '^DefaultDependencies=no' "$SOURCE"; then
    sed -i '/^DefaultDependencies=no$/d' "$DEST"
    sed -i '/^After=multi-user.target$/d' "$DEST"
fi

# Restart/timeout hardening — strip any existing keys, then insert fresh into [Service]
sed -i '/^Restart=/d' "$DEST"
sed -i '/^TimeoutStartSec=/d' "$DEST"
sed -i '/^\[Service\]$/a Restart=on-failure' "$DEST"
sed -i '/^\[Service\]$/a TimeoutStartSec=90s' "$DEST"

systemctl daemon-reload
