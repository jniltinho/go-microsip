#!/bin/sh
# Run inside a desktop VM as the login user (task 7.3): how long go-microsip's AppImage and the
# Rust candidate's take to start, and what they hold in memory, measured the same way on the same
# desktop, one after the other, each with a new, empty profile and no account.
#   /vagrant/measure.sh <version> [runs]
#
# Three moments are timed from the launch. The control socket is there: both make one, early.
# The application answers `status` on it, asked by its own command line every few hundredths of
# a second (so this moment is late by up to one question): the Rust candidate answers from its
# window's loop, so for it this is the window running; go-microsip answers before its window is
# drawn. go-microsip's window reports its first drawn page (the Rust candidate has no such
# report). Memory is taken 20 s after the start: the
# proportional set size of every process of the application's session (go-microsip's web view
# runs in processes of its own), and the resident set of its main process.
set -eu
version=$1
runs=${2:-10}
. /etc/os-release
session=$(pgrep -u "$(id -u)" -x gnome-shell | head -n 1)
[ -n "$session" ] || { echo "measure: no desktop session" >&2; exit 1; }
eval "$(tr '\0' '\n' <"/proc/$session/environ" | grep -E '^(DISPLAY|XAUTHORITY|WAYLAND_DISPLAY|XDG_SESSION_TYPE|XDG_CURRENT_DESKTOP|DBUS_SESSION_BUS_ADDRESS)=' | sed "s/^/export /")"
[ -n "${WAYLAND_DISPLAY:-}" ] || [ ! -S "$XDG_RUNTIME_DIR/wayland-0" ] || export WAYLAND_DISPLAY=wayland-0
sudo loginctl unlock-sessions 2>/dev/null || :
export APPIMAGE_EXTRACT_AND_RUN=1 # as the checks run them: no FUSE in these VMs
work=$(mktemp -d)
cp "/dist/go-microsip-$version-x86_64.AppImage" "$work/go.AppImage"
cp /dist/g0/rust-microsip.AppImage "$work/rust.AppImage"
chmod +x "$work/go.AppImage" "$work/rust.AppImage"
# The command line that asks is unpacked once: unpacking it at every question would be the wait.
for a in go rust; do
	mkdir "$work/$a" && (cd "$work/$a" && "$work/$a.AppImage" --appimage-extract >/dev/null 2>&1)
done
echo "measure: $PRETTY_NAME, $XDG_CURRENT_DESKTOP on $XDG_SESSION_TYPE, $(nproc) processors, $(awk '/MemTotal/ { printf "%d MB", $2 / 1024 }' /proc/meminfo)"
echo "measure: go-microsip $("$work/go.AppImage" version 2>/dev/null | head -n 1), $(stat -c %s "$work/go.AppImage") bytes"
echo "measure: Rust candidate $("$work/rust.AppImage" --version 2>/dev/null | head -n 1), $(stat -c %s "$work/rust.AppImage") bytes"

ms() { echo $((($(date +%s%N) - $1) / 1000000)); }
leave() {
	pkill -x go-microsip 2>/dev/null || :
	pkill -f '[a]ppimage_extracted_[0-9a-f]*/AppRun' 2>/dev/null || :
	i=0
	while pgrep -x go-microsip >/dev/null || pgrep -f '[a]ppimage_extracted_[0-9a-f]*/AppRun' >/dev/null; do
		i=$((i + 1))
		[ "$i" -lt 100 ] || { pkill -9 -x go-microsip; pkill -9 -f '[a]ppimage_extracted_[0-9a-f]*/AppRun'; break; }
		sleep 0.1
	done
}
# once <name> <app> <client>: one start. Sets started (ms to the control socket), answered (ms to
# an answered status), drawn (ms to the first page, go-microsip only) and sid (the application's
# session of processes).
once() {
	name=$1
	app=$2
	client=$3
	answered=""
	rm -rf "$work/profile" "$work/trace.jsonl"
	rm -f "$XDG_RUNTIME_DIR/$name/control.sock"
	drawn=""
	begin=$(date +%s%N)
	if [ "$name" = go-microsip ]; then
		XDG_DATA_HOME="$work/profile/data" XDG_CONFIG_HOME="$work/profile/config" setsid "$app" --ui-trace "$work/trace.jsonl" >"$work/app.log" 2>&1 </dev/null &
	else
		XDG_DATA_HOME="$work/profile/data" XDG_CONFIG_HOME="$work/profile/config" setsid "$app" >"$work/app.log" 2>&1 </dev/null &
	fi
	sid=$!
	started=""
	i=0
	while [ -z "$answered" ] || { [ "$name" = go-microsip ] && [ -z "$drawn" ]; }; do
		if [ -z "$started" ] && [ -S "$XDG_RUNTIME_DIR/$name/control.sock" ]; then started=$(ms "$begin"); fi
		if [ -n "$started" ] && [ -z "$answered" ] &&
			XDG_DATA_HOME="$work/profile/data" XDG_CONFIG_HOME="$work/profile/config" "$client" status --json 2>/dev/null | grep -q '"status":"ok"'; then
			answered=$(ms "$begin")
		fi
		if [ -z "$drawn" ] && grep -q '"page"' "$work/trace.jsonl" 2>/dev/null; then drawn=$(ms "$begin"); fi
		i=$((i + 1))
		if [ "$i" -gt 6000 ]; then
			echo "measure: FAILED: $name did not start within a minute" >&2
			tail -n 3 "$work/app.log" >&2
			leave
			exit 1
		fi
		sleep 0.01
	done
}
stats() { # the numbers on the standard input: median, least, most
	sort -n | awk '{ v[NR] = $1 } END { printf "median %d ms, least %d ms, most %d ms, %d starts", v[int((NR + 1) / 2)], v[1], v[NR], NR }'
}
for pair in "go-microsip go" "rust-microsip rust"; do
	set -- $pair
	name=$1
	app=$work/$2.AppImage
	client=$work/$2/squashfs-root/AppRun
	leave
	sudo sh -c 'sync; echo 3 > /proc/sys/vm/drop_caches'
	once "$name" "$app" "$client"
	echo "measure: $name: first start with the file cache dropped: $started ms to the control socket, $answered ms to an answered status${drawn:+, $drawn ms to the first drawn page}"
	leave
	: >"$work/started"
	: >"$work/answered"
	: >"$work/drawn"
	n=0
	while [ "$n" -lt "$runs" ]; do
		once "$name" "$app" "$client"
		echo "$started" >>"$work/started"
		echo "$answered" >>"$work/answered"
		[ -z "$drawn" ] || echo "$drawn" >>"$work/drawn"
		n=$((n + 1))
		[ "$n" -eq "$runs" ] || leave
	done
	echo "measure: $name: to the control socket: $(stats <"$work/started")"
	echo "measure: $name: to an answered status: $(stats <"$work/answered")"
	[ ! -s "$work/drawn" ] || echo "measure: $name: to the first drawn page: $(stats <"$work/drawn")"
	# The last start stays up: memory after 20 s at rest.
	sleep 20
	total=0
	count=0
	unread=0
	for pid in $(ps -o pid= -s "$sid"); do
		if pss=$(awk '/^Pss:/ { print $2 }' "/proc/$pid/smaps_rollup" 2>/dev/null) && [ -n "$pss" ]; then
			total=$((total + pss))
			count=$((count + 1))
		else
			unread=$((unread + 1))
		fi
	done
	main=$(pgrep -s "$sid" -x go-microsip | head -n 1)
	[ -n "$main" ] || main=$(pgrep -s "$sid" -f 'appimage_extracted_[0-9a-f]*/' | head -n 1)
	rss=$(awk '/^VmRSS:/ { print $2 }' "/proc/$main/status" 2>/dev/null || echo 0)
	echo "measure: $name: memory 20 s after the start: $((total / 1024)) MB proportional in $count processes$( [ "$unread" -eq 0 ] || echo " ($unread not readable)"), $((rss / 1024)) MB resident in the main one"
	leave
done
rm -rf "$work"
