#!/bin/bash
# Apply the stratisd ordering fix
/usr/libexec/fix-stratisd-ordering.sh

# Enable the path watcher for future stratisd updates
systemctl enable --now stratisd-fix-ordering.path 2>/dev/null || true

systemctl enable --now podman-volume-stratis.socket 2>/dev/null || true

systemctl daemon-reload
