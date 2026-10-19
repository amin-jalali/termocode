#!/bin/sh
if [ "$1" = "-version" ]; then
	echo '3.45.1 2024-01-30'
	exit 0
fi
while IFS= read -r line; do
	case "$line" in
	SELECT\ SLOW*) sleep 10 ;;
	SELECT*) printf '[{"z":2,"a":1}]\n' ;;
	.print\ __TC_END_*) printf '%s\n' "${line#*.print }" ;;
	*) : ;;
	esac
done
