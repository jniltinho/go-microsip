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
# The fence is Docker's own chain for the administrator's rules (DOCKER-USER), which every packet
# forwarded to or from a container passes first: a rule pair per client, with the RTP range apart
# so that it is counted, and everything else dropped, the containers' way out included.
set -eu
image=rust-microsip-asterisk:22.11.0-lab.1
pbx() { docker exec rust-microsip-lab-pbx-1 asterisk -C /run/asterisk/config/asterisk.conf -rx "$1"; }
case ${1:-} in
up)
	bind=$2
	shift 2
	docker image inspect "$image" >/dev/null 2>&1 || docker load -q -i /dist/g0/asterisk-lab.tar
	card=$(ip -o -4 addr show | awk -v a="$bind" 'index($4, a "/") == 1 { print $2 }')
	[ -n "$card" ] || { echo "vm-lab: no network card has $bind" >&2; exit 1; }
	sudo iptables -F DOCKER-USER
	for client in "$@"; do
		sudo iptables -A DOCKER-USER -i "$card" -s "$client" -p udp --dport 10000:10019 -j RETURN
		sudo iptables -A DOCKER-USER -o "$card" -d "$client" -p udp --sport 10000:10019 -j RETURN
		sudo iptables -A DOCKER-USER -i "$card" -s "$client" -j RETURN
		sudo iptables -A DOCKER-USER -o "$card" -d "$client" -j RETURN
	done
	sudo iptables -A DOCKER-USER -j DROP
	LAB_BIND_IP=$bind sh /srv/docker/asterisk/lab.sh vm-up
	echo "vm-lab: the PBX answers on $bind for $*"
	;;
ring)
	for _ in $(seq 1800); do
		if pbx "pjsip show contacts" | grep -q "Contact: *$2/"; then
			sleep 3 # the softphone has just registered: its window is still coming up
			if pbx "channel originate PJSIP/$2 application Echo" | grep -q "No such command"; then
				echo "vm-lab: FAILED: this PBX cannot originate a call" >&2
				exit 1
			fi
			echo "vm-lab: the PBX called $2"
			exit 0
		fi
		sleep 1
	done
	echo "vm-lab: $2 never registered" >&2
	exit 1
	;;
report)
	sudo iptables -L DOCKER-USER -v -n -x | awk '
		/dpts:10000:10019/ { print "vm-lab: RTP packets from " $8 " to the PBX: " $1 }
		/spts:10000:10019/ { print "vm-lab: RTP packets from the PBX to " $9 ": " $1 }
		$3 == "DROP" { print "vm-lab: packets of anyone else dropped: " $1 }'
	;;
down)
	sh /srv/docker/asterisk/lab.sh vm-down
	sudo iptables -F DOCKER-USER
	;;
*)
	echo "usage: pbx.sh up <address> <client>... | ring <account> | report | down" >&2
	exit 2
	;;
esac
