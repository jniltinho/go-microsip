#!/bin/sh
# Run inside the auxiliary "pbx" VM by run.sh: the disposable Asterisk lab for calls from the
# desktop VMs (docker/asterisk, profile "vm").
#
#   pbx.sh up <address> <client>...   start the PBX on this VM's isolated address, reachable by
#                                     the clients named and by nothing else
#   pbx.sh ring <account>             wait for the account to register, then call it (the PBX
#                                     echoes what the softphone sends once it answers)
#   pbx.sh report                     what the fence counted: the clients' RTP in and out
#   pbx.sh down                       stop the PBX and take the fence away
#
# The fence is a chain of its own that Docker's chain for the administrator's rules (DOCKER-USER)
# sends every packet forwarded to or from a container through first: a rule pair per client, with
# the RTP range apart so that it is counted, and everything else dropped, the containers' way out
# included. It is not kept across a restart of this VM, and neither is the PBX.
set -eu
image=go-microsip-asterisk:22.11.0-lab.1
pbx() { docker exec go-microsip-lab-pbx-1 asterisk -C /run/asterisk/config/asterisk.conf -rx "$1"; }
# unfence takes the fence named LAB-FENCE away: the jump to it, then the chain.
unfence() {
	while sudo iptables -C DOCKER-USER -j LAB-FENCE 2>/dev/null; do sudo iptables -D DOCKER-USER -j LAB-FENCE; done
	if sudo iptables -S LAB-FENCE >/dev/null 2>&1; then
		sudo iptables -F LAB-FENCE
		sudo iptables -X LAB-FENCE
	fi
}
case ${1:-} in
up)
	bind=$2
	shift 2
	docker image inspect "$image" >/dev/null 2>&1 || docker load -q -i /dist/g0/asterisk-lab.tar
	card=$(ip -o -4 addr show | awk -v a="$bind" 'index($4, a "/") == 1 { print $2 }')
	[ -n "$card" ] || { echo "vm-lab: no network card has $bind" >&2; exit 1; }
	for client in "$@"; do
		echo "$client" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$' || { echo "vm-lab: not an address: $client" >&2; exit 1; }
	done
	# The new fence is whole, its last rule included, before the PBX's packets are sent through
	# it, and the one before it goes only then: a failure on the way leaves the old one standing.
	new=LAB-FENCE-$$
	sudo iptables -N "$new"
	for client in "$@"; do
		sudo iptables -A "$new" -i "$card" -s "$client" -p udp --dport 10000:10019 -j RETURN
		sudo iptables -A "$new" -o "$card" -d "$client" -p udp --sport 10000:10019 -j RETURN
		sudo iptables -A "$new" -i "$card" -s "$client" -j RETURN
		sudo iptables -A "$new" -o "$card" -d "$client" -j RETURN
	done
	sudo iptables -A "$new" -j DROP
	sudo iptables -I DOCKER-USER 1 -j "$new"
	unfence
	sudo iptables -E "$new" LAB-FENCE
	LAB_BIND_IP=$bind sh /srv/docker/asterisk/lab.sh vm-up
	echo "vm-lab: the PBX answers on $bind for $*"
	;;
ring)
	for _ in $(seq 1800); do
		if pbx "pjsip show contacts" | grep -q "Contact: *$2/"; then
			sleep 3 # the softphone has just registered: its window is still coming up
			if ! said=$(pbx "channel originate PJSIP/$2 application Echo") || echo "$said" | grep -qi "no such command\|unable\|fail"; then
				echo "vm-lab: FAILED: the PBX did not originate a call to $2"
				exit 1
			fi
			echo "vm-lab: the PBX called $2"
			# A second call, once the first is over, that the PBX gives up after six seconds: a
			# call nobody answered.
			ext=$2 # a function has positional parameters of its own
			up() { pbx "core show channels concise" | grep -q "^PJSIP/$ext-"; }
			for _ in $(seq 60); do up && break; sleep 1; done
			for _ in $(seq 300); do up || break; sleep 1; done
			sleep 4
			if ! said=$(pbx "channel originate PJSIP/$2 application Echo") || echo "$said" | grep -qi "no such command\|unable\|fail"; then
				echo "vm-lab: FAILED: the PBX did not originate the second call to $2"
				exit 1
			fi
			sleep 6
			pbx "channel request hangup all" >/dev/null
			echo "vm-lab: the PBX called $2 again and gave up after six seconds"
			exit 0
		fi
		sleep 1
	done
	echo "vm-lab: FAILED: $2 never registered"
	exit 1
	;;
report)
	counted=$(sudo iptables -L LAB-FENCE -v -n -x) || { echo "vm-lab: FAILED: the fence could not be read"; exit 1; }
	echo "$counted" | awk '
		/dpts:10000:10019/ { print "vm-lab: RTP packets from " $8 " to the PBX: " $1 }
		/spts:10000:10019/ { print "vm-lab: RTP packets from the PBX to " $9 ": " $1 }
		$3 == "DROP" { print "vm-lab: packets of anyone else dropped: " $1 }'
	;;
down)
	sh /srv/docker/asterisk/lab.sh vm-down
	unfence
	;;
*)
	echo "usage: pbx.sh up <address> <client>... | ring <account> | report | down" >&2
	exit 2
	;;
esac
