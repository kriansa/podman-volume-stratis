#!/bin/bash
systemctl stop podman-volume-stratis 2>/dev/null || true
systemctl disable podman-volume-stratis 2>/dev/null || true
