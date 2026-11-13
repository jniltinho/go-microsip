#!/bin/sh
# Run as root inside the "freepbx" VM by interop.sh (task 7.1): FreePBX 17 as the lab's
# interoperability target.
#
#   freepbx.sh setup <client>...   the lab's fictional extensions 1001 and 1002 with the lab's
#                                  passwords, the lab's test routes, and the fence: the clients
#                                  named reach this VM on the lab's network, nobody else does
#   freepbx.sh versions            what is installed
#
# The extensions go through FreePBX's own bulk import and the routes into the file FreePBX keeps
# for an administrator's dialplan, so the calls take FreePBX's generated dialplan, not one
# written here. The passwords are read from the lab's folder and never printed.
set -eu
case ${1:-} in
setup)
	shift
	umask 077
	csv=$(mktemp)
	trap 'rm -f "$csv"' EXIT
	python3 - >"$csv" <<'PY'
import json
accounts = json.load(open("/srv/lab/credentials.json"))
print("extension,name,tech,secret")
for number in ("1001", "1002"):
    print(f"{number},Lab {number},pjsip,{accounts[number]}")
PY
	chown asterisk:asterisk "$csv"
	fwconsole bulkimport --type=extensions "$csv" --replace >/dev/null
	# The lab's routes (docker/asterisk/lab.py): echo, the three-digit question, a steady tone.
	cat >/etc/asterisk/extensions_custom.conf <<'EOF'
[from-internal-custom]
exten => 600,1,Answer()
 same => n,Echo()
 same => n,Hangup()
exten => 601,1,Answer()
 same => n,Read(DIGITS,,3,,1,8)
 same => n,GotoIf($["${DIGITS}" = "123"]?accepted:rejected)
 same => n(accepted),PlayTones(1000)
 same => n,Wait(3)
 same => n,Hangup()
 same => n(rejected),PlayTones(400)
 same => n,Wait(3)
 same => n,Hangup()
exten => 602,1,Answer()
 same => n,PlayTones(1000)
 same => n,Wait(20)
 same => n,Hangup()
EOF
	chown asterisk:asterisk /etc/asterisk/extensions_custom.conf
	fwconsole reload >/dev/null
	card=$(ip -o -4 addr show | awk '$4 ~ /^192\.168\.77\./ { print $2 }')
	[ -n "$card" ] || { echo "freepbx: no network card on the lab's network" >&2; exit 1; }
	for client in "$@"; do
		echo "$client" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$' || { echo "freepbx: not an address: $client" >&2; exit 1; }
	done
	# The new fence is whole before packets go through it, and the old one goes only then.
	new=LAB-FENCE-$$
	iptables -N "$new"
	for client in "$@"; do
		iptables -A "$new" -s "$client" -j RETURN
	done
	iptables -A "$new" -j DROP
	iptables -I INPUT 1 -i "$card" -j "$new"
	while iptables -C INPUT -i "$card" -j LAB-FENCE 2>/dev/null; do iptables -D INPUT -i "$card" -j LAB-FENCE; done
	if iptables -S LAB-FENCE >/dev/null 2>&1; then
		iptables -F LAB-FENCE
		iptables -X LAB-FENCE
	fi
	iptables -E "$new" LAB-FENCE
	# Nothing of the lab speaks IPv6: on that card it is refused outright.
	ip6tables -C INPUT -i "$card" -j DROP 2>/dev/null || ip6tables -I INPUT 1 -i "$card" -j DROP
	# Kept across a restart of this VM, where FreePBX starts by itself: the installer brings the
	# service that restores saved rules before the network is up.
	netfilter-persistent save >/dev/null 2>&1 || { echo "freepbx: FAILED: the fence could not be saved for the next start"; exit 1; }
	echo "freepbx: extensions 1001 and 1002, the lab's routes 600 to 602, reachable on $card by $*"
	;;
versions)
	echo "freepbx: $(fwconsole --version 2>/dev/null | tail -n 1)"
	echo "freepbx: $(asterisk -V)"
	echo "freepbx: $(. /etc/os-release && echo "$PRETTY_NAME")"
	asterisk -rx "pjsip show transports" | awk '/^ *Transport:/ && $2 != "<TransportId........>" { print "freepbx: transport " $2 " " $3 " " $NF }'
	;;
*)
	echo "usage: freepbx.sh setup <client>... | versions" >&2
	exit 2
	;;
esac
