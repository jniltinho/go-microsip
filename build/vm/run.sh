#!/bin/sh
# Bring up each desktop VM, run check.sh in it, save the output and destroy the VM.
#   [KEEP=1] [LAB=1] build/vm/run.sh <version> <receipt dir> [audio seconds] [vm names...]
#
# LAB=1 also places calls from the Windows VM (the Vagrantfile says how): the lab's fictional
# accounts are made once under artifacts/vm-lab/ (remove that directory by hand for new ones), the
# lab image goes to the "pbx" VM as a file, and the PBX there is fenced to the desktop VM's
# address. Before the calls, the fence is shown to hold for an address that is not on the list.
set -eu
cd "$(dirname "$0")"
version=$1
out=$2
seconds=${3:-300}
shift 3 2>/dev/null || shift $#
[ $# -gt 0 ] || set -- ubuntu2604 debian13
status=0
lab_pbx=192.168.77.10
lab_windows=192.168.77.20
lab_image=rust-microsip-asterisk:22.11.0-lab.1
pbx() { vagrant ssh pbx -c "/vagrant/pbx.sh $*"; }
# Whether the Windows VM gets a TCP connection to the PBX's SIP port.
reaches() { vagrant winrm windows11 -s powershell -c "(Test-NetConnection $lab_pbx -Port 5060 -WarningAction SilentlyContinue).TcpTestSucceeded" 2>/dev/null | grep -q True; }
if [ -n "${LAB:-}" ]; then
	lab=../../artifacts/vm-lab
	if [ ! -d "$lab" ]; then
		mkdir -p -m 700 "$lab"
		docker run --rm --network none --user "$(id -u):$(id -g)" --read-only --cap-drop ALL \
			--security-opt no-new-privileges --tmpfs /tmp:rw,noexec,nosuid,nodev,mode=1777,size=32m \
			--mount "type=bind,source=$(cd "$lab" && pwd),target=/lab" "$lab_image" generate >/dev/null
	fi
	mkdir -p ../../dist/go/g0
	docker save "$lab_image" -o ../../dist/go/g0/asterisk-lab.tar
	vagrant up pbx >"$out/vm-pbx-provision.log" 2>&1
	{
		pbx down || : # what an earlier run left: this PBX starts from the image as it is now
		vagrant ssh pbx -c "docker image rm -f $lab_image" || :
		pbx up "$lab_pbx" 192.168.77.99 # a fence that leaves the desktop VM out
	} >"$out/vm-lab.log" 2>&1
fi
for vm in "$@"; do
	log=$out/vm-$vm.log
	rm -f "$log" # a VM that does not come up must not be reported with the last run's log
	with_pbx=""
	case $vm in
	windows*)
		if [ -n "${LAB:-}" ] && vagrant up "$vm" >"$out/vm-$vm-provision.log" 2>&1; then
			with_pbx=" -Pbx $lab_pbx"
			{
				if reaches; then echo "vm-lab: FAILED: the PBX answers an address that is not on its list"; else echo "vm-lab: the PBX does not answer $lab_windows while its list names another address"; fi
				pbx up "$lab_pbx" "$lab_windows"
				if reaches; then echo "vm-lab: the PBX answers $lab_windows once it is on the list"; else echo "vm-lab: FAILED: the PBX does not answer $lab_windows"; fi
			} >>"$out/vm-lab.log" 2>&1
			pbx ring 1002 >>"$out/vm-lab.log" 2>&1 & # the call the window is to answer, when 1002 registers
			ring=$!
		fi
		check="vagrant winrm $vm -s powershell -c \"C:\\vagrant\\check.ps1 -Version $version -Seconds $seconds$with_pbx\""
		;;
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
	if [ -n "$with_pbx" ]; then
		kill "$ring" 2>/dev/null || : # still waiting: the check ended before the window registered
		wait "$ring" 2>/dev/null || :
		pbx report >>"$out/vm-lab.log" 2>&1 || :
		grep '^vm-lab:' "$out/vm-lab.log" | tee -a "$log"
		grep -q '^vm-lab: FAILED' "$out/vm-lab.log" && status=1
	fi
	# KEEP=1 leaves the VM up for a closer look; destroy it with `vagrant destroy -f`.
	[ -n "${KEEP:-}" ] || vagrant destroy -f "$vm" >/dev/null 2>&1 || :
done
if [ -n "${LAB:-}" ]; then
	pbx down >>"$out/vm-lab.log" 2>&1 || :
	[ -n "${KEEP:-}" ] || vagrant destroy -f pbx >/dev/null 2>&1 || :
fi
exit $status
