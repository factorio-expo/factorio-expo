#!/bin/sh
set -eu

for process in /proc/[0-9]*/comm; do
    if IFS= read -r name < "$process" && [ "$name" = "factorio" ]; then
        exit 0
    fi
done

exit 1
