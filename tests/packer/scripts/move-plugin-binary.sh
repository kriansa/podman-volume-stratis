#!/bin/bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <source-path>" >&2
    exit 1
fi

SOURCE_PATH="$1"
DEST_PATH="/usr/local/bin/podman-volume-stratis"

if [[ ! -f "$SOURCE_PATH" ]]; then
    echo "Error: Source file not found: $SOURCE_PATH" >&2
    exit 1
fi

mv -f "$SOURCE_PATH" "$DEST_PATH"
chown root:root "$DEST_PATH"
chmod 755 "$DEST_PATH"
restorecon -v "$DEST_PATH"
