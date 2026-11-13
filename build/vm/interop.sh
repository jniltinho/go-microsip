#!/bin/sh
# FreePBX 17 interoperability (task 7.1, make go-freepbx): the lab's scenarios against a FreePBX
# installed in the "freepbx" VM, run from the auxiliary "pbx" VM on the lab's internal network.
#   [KEEP=1] build/vm/interop.sh <receipt dir>
#
# The fictional accounts are those of the VM lab (artifacts/vm-lab/, made by run.sh's rule when
# missing). FreePBX gets the two UDP ones as its extensions; the scenarios register them and
# place the calls: echo in both codecs, a call between the two, hold and mute, a blind
# transfer, DTMF with its control, do-not-disturb during a call, a renewal; then the calls again
# with the network of both VMs impaired (impair.sh). Without KEEP=1 both VMs are destroyed when the run ends,
# however it ends.
set -eu
cd "$(dirname "$0")"
out=$1
lab_pbx=192.168.77.10
lab_freepbx=192.168.77.11
lab_image=rust-microsip-asterisk:22.11.0-lab.1
scenarios=register/1001,engine/echo/1001/,engine/pair/1001-1002,engine/hold-mute/1001-1002,engine/transfer/1001-1002,engine/dtmf/1001,engine/dtmf/wrong-digits-control,engine/dnd/during-a-call,engine/renewal/1001
# The calls made again on a network that loses, delays and reorders (impair.sh): they must stay
# controllable, with their audio.
degraded=engine/echo/1001/,engine/pair/1001-1002,engine/hold-mute/1001-1002,engine/transfer/1001-1002,engine/dtmf/1001,engine/dtmf/wrong-digits-control,engine/dnd/during-a-call
lab=../../artifacts/vm-lab
if [ ! -d "$lab" ]; then
	mkdir -p -m 700 "$lab"
	docker run --rm --network none --user "$(id -u):$(id -g)" --read-only --cap-drop ALL \
		--security-opt no-new-privileges --tmpfs /tmp:rw,noexec,nosuid,nodev,mode=1777,size=32m \
		--mount "type=bind,source=$(cd "$lab" && pwd),target=/lab" "$lab_image" generate >/dev/null
fi
mkdir -p "$out"
# What this run brought up goes with it, however it ends; KEEP=1 leaves the VMs up.
cleanup() {
	code=$?
	for vm in pbx freepbx; do # a network left impaired would spoil the next run of a kept VM
		vagrant ssh "$vm" -c "/vagrant/impair.sh off" >/dev/null 2>&1 || :
	done
	if [ -z "${KEEP:-}" ]; then
		for vm in freepbx pbx; do
			vagrant destroy -f "$vm" >/dev/null 2>&1 || echo "freepbx: $vm was not destroyed: vagrant destroy -f $vm" >&2
		done
	fi
	exit "$code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
vagrant up pbx >"$out/vm-pbx-provision.log" 2>&1
vagrant up freepbx >"$out/vm-freepbx-provision.log" 2>&1
log=$out/freepbx.log
status=0
{
	# The Asterisk lab on the auxiliary VM is off: the scenarios' endpoints use that VM's address.
	vagrant ssh pbx -c "/vagrant/pbx.sh down" >/dev/null 2>&1 || :
	vagrant ssh freepbx -c "sudo /vagrant/freepbx.sh versions; sudo /vagrant/freepbx.sh setup $lab_pbx"
	# The fence, seen from both sides: the auxiliary VM is listed, the Windows VM is not.
	if vagrant ssh pbx -c "curl -s -m 10 -o /dev/null http://$lab_freepbx/"; then
		echo "freepbx: the listed client reaches FreePBX"
	else
		echo "freepbx: FAILED: the listed client does not reach FreePBX"
	fi
	if vagrant status windows11 2>/dev/null | grep -q running; then
		said=$(vagrant winrm windows11 -s powershell -c "'reached=' + (Test-NetConnection $lab_freepbx -Port 80 -WarningAction SilentlyContinue).TcpTestSucceeded" 2>/dev/null) || said=""
		case $said in
		*reached=False*) echo "freepbx: a VM of the lab's network that is not listed gets no connection" ;;
		*reached=True*) echo "freepbx: FAILED: FreePBX answers a VM that is not on its list" ;;
		*) echo "freepbx: NOT VERIFIED: the Windows VM was not asked whether it reaches FreePBX" ;;
		esac
	else
		echo "freepbx: NOT VERIFIED: no VM that is not listed is up to try the fence from"
	fi
} >"$log" 2>&1
vagrant ssh pbx -c "/dist/g0/g0lab -lab /srv/lab-clients -server $lab_freepbx -run $scenarios" \
	>"$out/freepbx-report.json" 2>"$out/freepbx-runner.log" || status=$?
grep '^freepbx:' "$log" || :
grep -q '^freepbx: FAILED' "$log" && status=1
python3 -c 'import json, sys
r = json.load(open(sys.argv[1]))
for s in r["results"]:
    print("freepbx:", "PASS" if s["ok"] else "FAIL", s["name"], "" if s["ok"] else s.get("detail", ""))
print("freepbx:", r["scenarios"], "scenarios,", r["failed"], "failed")' "$out/freepbx-report.json" | tee -a "$log" || status=1

for vm in pbx freepbx; do
	vagrant ssh "$vm" -c "/vagrant/impair.sh on" >>"$log" 2>&1 || { echo "freepbx: FAILED: the network of $vm was not impaired" | tee -a "$log"; status=1; }
done
vagrant ssh pbx -c "/dist/g0/g0lab -lab /srv/lab-clients -server $lab_freepbx -run $degraded" \
	>"$out/freepbx-degraded-report.json" 2>"$out/freepbx-degraded-runner.log" || status=$?
for vm in pbx freepbx; do
	vagrant ssh "$vm" -c "/vagrant/impair.sh show; /vagrant/impair.sh off" 2>/dev/null | grep '^impair:' | tee -a "$log" || status=1
done
python3 -c 'import json, sys
r = json.load(open(sys.argv[1]))
for s in r["results"]:
    print("freepbx: degraded:", "PASS" if s["ok"] else "FAIL", s["name"], "" if s["ok"] else s.get("detail", ""))
print("freepbx: degraded:", r["scenarios"], "scenarios,", r["failed"], "failed")' "$out/freepbx-degraded-report.json" | tee -a "$log" || status=1
exit $status
