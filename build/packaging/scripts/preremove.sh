#!/bin/bash
# rpm runs this before removing the package, and also during an upgrade, before removing the old
# version, once the new one is already installed. Its first argument is how many copies of the
# package will be left: 0 when the package is being removed, 1 during an upgrade.
#
# Only a removal undoes anything. During an upgrade, stopping the plugin would also stop every
# volume that requires it, and every container using one, with nothing to start them again. And
# removing the stratisd override would undo what the new version has just set up.
if [ "${1:-0}" -ge 1 ]; then
    exit 0
fi

systemctl stop podman-volume-stratis 2>/dev/null || true
systemctl disable podman-volume-stratis 2>/dev/null || true

# Stop and disable the path watcher
systemctl stop stratisd-fix-ordering.path 2>/dev/null || true
systemctl disable stratisd-fix-ordering.path 2>/dev/null || true

# Remove the stratisd.service override we created to fix the ordering cycle
rm -f /etc/systemd/system/stratisd.service

systemctl daemon-reload
