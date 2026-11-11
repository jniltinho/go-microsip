#!/bin/sh
# Bring up each desktop VM, run check.sh in it, save the output and destroy the VM.
#   build/vm/run.sh <version> <receipt dir> [audio seconds] [vm names...]
set -eu
cd "$(dirname "$0")"
version=$1
out=$2
seconds=${3:-300}
shift 3 2>/dev/null || shift $#
[ $# -gt 0 ] || set -- ubuntu2604 debian13
status=0
for vm in "$@"; do
	log=$out/vm-$vm.log
	if vagrant up "$vm" >"$out/vm-$vm-provision.log" 2>&1 &&
		vagrant ssh "$vm" -c "/vagrant/check.sh $version $seconds" >"$log" 2>&1; then
		grep '^vm-check:' "$log" | tail -n 1
	else
		status=1
		echo "vm-check: FAIL $vm (see $log)"
		tail -n 12 "$log" 2>/dev/null || tail -n 12 "$out/vm-$vm-provision.log"
	fi
	vagrant destroy -f "$vm" >/dev/null 2>&1 || :
done
exit $status
