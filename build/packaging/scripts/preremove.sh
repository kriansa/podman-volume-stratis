#!/bin/bash
systemctl stop podman-volume-stratis.socket 2>/dev/null || true
systemctl disable podman-volume-stratis.socket 2>/dev/null || true
systemctl stop podman-volume-stratis 2>/dev/null || true
systemctl disable podman-volume-stratis 2>/dev/null || true

# Stop and disable the path watcher
systemctl stop stratisd-fix-ordering.path 2>/dev/null || true
systemctl disable stratisd-fix-ordering.path 2>/dev/null || true

# Remove the stratisd.service override we created to fix the ordering cycle
rm -f /etc/systemd/system/stratisd.service

systemctl daemon-reload
