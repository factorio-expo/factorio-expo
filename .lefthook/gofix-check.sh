#!/bin/sh
# Fails when `go fix` would change the packages of the given Go files.
dirs=$(for f in "$@"; do dirname "$f"; done | sort -u | sed 's|^|./|')
# shellcheck disable=SC2086
diff=$(go fix -diff $dirs 2>&1) || { echo "$diff" >&2; exit 1; }
if [ -n "$diff" ]; then
	echo "go fix would change these packages (run go fix on them):" >&2
	echo "$diff" >&2
	exit 1
fi
