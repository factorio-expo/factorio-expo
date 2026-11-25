#!/bin/sh
# pre-push: reads "<local ref> <local sha> <remote ref> <remote sha>" lines
# from stdin and refuses any push that updates or deletes master.
while read -r local_ref local_sha remote_ref remote_sha; do
	if [ "$remote_ref" = "refs/heads/master" ]; then
		echo "Direct pushes to master are blocked. Push a branch and merge it instead." >&2
		exit 1
	fi
done
exit 0
