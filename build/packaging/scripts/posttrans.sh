#!/bin/bash
# rpm runs this once an install or upgrade is complete: after the new version's files are in
# place, and after the old version's removal script. That order matters on an upgrade from 1.1.4
# or older, whose removal script undid the first two steps below even during an upgrade.

# Apply the stratisd ordering fix
/usr/libexec/fix-stratisd-ordering.sh

# Enable the path watcher for future stratisd updates
systemctl enable --now stratisd-fix-ordering.path 2>/dev/null || true

systemctl daemon-reload

# Run the new version, if the plugin is running; on a first install it is not yet, and nothing
# happens. Every volume and container that requires the plugin restarts along with it and comes
# back by itself. --no-block, so the package manager does not wait for all of them.
systemctl try-restart --no-block podman-volume-stratis 2>/dev/null || true
