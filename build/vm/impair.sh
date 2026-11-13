#!/bin/sh
# Run inside a lab VM (task 7.4, the Rust-era scenario AUD-02): what this VM sends on the lab's
# network is delayed by 0 to 30 ms, loses 1 % of its packets and has a tenth of them sent out of
# order. interop.sh turns it on in both VMs of a call, so each direction is impaired.
#   impair.sh on | off | show
set -eu
card=$(ip -o -4 addr show | awk '$4 ~ /^192\.168\.77\./ { print $2 }')
[ -n "$card" ] || { echo "impair: no network card on the lab's network" >&2; exit 1; }
case ${1:-} in
on) sudo tc qdisc replace dev "$card" root netem delay 15ms 15ms loss 1% reorder 10% ;;
off) sudo tc qdisc del dev "$card" root 2>/dev/null || : ;;
show)
	sudo tc -s qdisc show dev "$card" | awk -v host="$(hostname -s)" '
		/netem/ { rule = $0; sub(/^qdisc /, "", rule) }
		/Sent/ { print "impair: " host ": " rule "; sent " $4 " packets, dropped " $7 + 0 }'
	;;
*)
	echo "usage: impair.sh on | off | show" >&2
	exit 2
	;;
esac
