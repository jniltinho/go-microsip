#!/bin/sh
# Bring up each desktop VM, run check.sh in it, save the output and destroy the VM.
#   [KEEP=1] build/vm/run.sh <version> <receipt dir> [audio seconds] [vm names...]
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
	rm -f "$log" # a VM that does not come up must not be reported with the last run's log
	case $vm in
	windows*) check="vagrant winrm $vm -s powershell -c \"C:\\vagrant\\check.ps1 -Version $version -Seconds $seconds\"" ;;
	*) check="vagrant ssh $vm -c \"/vagrant/check.sh $version $seconds\"" ;;
	esac
	if vagrant up "$vm" >"$out/vm-$vm-provision.log" 2>&1 && eval "$check" >"$log" 2>&1 &&
		grep -q '^vm-check: PASS' "$log"; then
		grep '^vm-check:' "$log" | tail -n 1
	else
		status=1
		echo "vm-check: FAIL $vm (see $log)"
		tail -n 12 "$log" 2>/dev/null || tail -n 12 "$out/vm-$vm-provision.log"
	fi
	# What the VM's screen shows at the end, for the receipt.
	id=$(cat ".vagrant/machines/$vm/virtualbox/id" 2>/dev/null) &&
		VBoxManage controlvm "$id" screenshotpng "$out/vm-$vm-screen.png" 2>/dev/null || :
	# KEEP=1 leaves the VM up for a closer look; destroy it with `vagrant destroy -f`.
	[ -n "${KEEP:-}" ] || vagrant destroy -f "$vm" >/dev/null 2>&1 || :
done
exit $status
