#!/bin/sh
# Fails when any of the given Go files is not gofmt-formatted.
unformatted=$(gofmt -l "$@")
if [ -n "$unformatted" ]; then
	echo "Not gofmt-formatted (run gofmt -w on them):" >&2
	echo "$unformatted" >&2
	exit 1
fi
