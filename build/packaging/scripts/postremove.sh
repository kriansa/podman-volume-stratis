#!/bin/bash
# rpm runs this after removing the package's files. On a removal (0 copies left), systemd still has
# the plugin's units loaded from files that are now gone, so it reloads. An upgrade reloads in
# posttrans instead.
if [ "${1:-0}" -ge 1 ]; then
    exit 0
fi

systemctl daemon-reload
