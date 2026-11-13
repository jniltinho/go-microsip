#!/bin/sh
# Controls of build/vm/mac.sh, with stand-ins for ssh and scp that say everything a real one
# could: the address, the user name, a host key, paths of the Mac's user. Whatever mac.sh prints
# or leaves behind is then searched for them. Fictional values only; needs no Mac and no network.
#   sh build/vm/mac-selftest.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd -P)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
address=203.0.113.77
login=leaky-user
key=AAAAC3FictionalHostKeyMaterial
mkdir "$work/bin" "$work/known"
printf '[%s]:2200 ssh-ed25519 %s\n' "$address" "$key" >"$work/known/hosts"
printf 'not a program\n' >"$work/go-microsip-darwin-amd64"

# The stand-in refuses to run without the options that keep ssh from asking or trusting, says
# what a real ssh says on its error stream, and answers as the mode tells it.
cat >"$work/bin/ssh" <<'EOF'
#!/bin/sh
all=" $* "
for needed in BatchMode=yes StrictHostKeyChecking=yes PasswordAuthentication=no \
	PreferredAuthentications=publickey KbdInteractiveAuthentication=no GlobalKnownHostsFile=/dev/null; do
	case $all in *" $needed "*) ;; *)
		echo "stand-in: $needed is missing" >>"$MAC_SELFTEST_NOTES"
		exit 99
		;;
	esac
done
case $all in *" UserKnownHostsFile=$MAC_SELFTEST_KNOWN "*) ;; *)
	echo "stand-in: the operator's host-key file is not the one used" >>"$MAC_SELFTEST_NOTES"
	exit 99
	;;
esac
for last; do :; done
echo "Warning: Permanently added '[$MAC_SELFTEST_ADDRESS]:2200' (ED25519) $MAC_SELFTEST_KEY" >&2
echo "debug: /Users/$MAC_SELFTEST_LOGIN/Library/Application Support/go-microsip"
case $MAC_SELFTEST_MODE in
unknown-key)
	echo "No ED25519 host key is known for [$MAC_SELFTEST_ADDRESS]:2200 and you have requested strict checking." >&2
	exit 255
	;;
password)
	echo "$MAC_SELFTEST_LOGIN@$MAC_SELFTEST_ADDRESS: Permission denied (publickey,password,keyboard-interactive)." >&2
	exit 255
	;;
esac
case $last in
*sw_vers*)
	if [ "$MAC_SELFTEST_MODE" = garbage ]; then
		printf 'macos=%s\narch=%s\nfolder=%s\n' "$MAC_SELFTEST_LOGIN" "$MAC_SELFTEST_ADDRESS" "/Users/$MAC_SELFTEST_LOGIN"
	else
		printf 'macos=15.8.1\narch=x86_64\nfolder=/tmp/go-microsip.Ab12Cd\n'
	fi
	;;
*" version") [ "$MAC_SELFTEST_MODE" = garbage-version ] && echo "$MAC_SELFTEST_LOGIN@$MAC_SELFTEST_ADDRESS" || echo "go-microsip 0.0.0 (0123456789ab)" ;;
*headless.sh*) [ "$MAC_SELFTEST_MODE" = failing ] && exit 1 ;;
*--self-test*) if [ "$MAC_SELFTEST_MODE" = interrupted ]; then kill -INT "$PPID"; sleep 1; fi ;;
esac
exit 0
EOF
cat >"$work/bin/scp" <<'EOF'
#!/bin/sh
echo "scp: $MAC_SELFTEST_LOGIN@$MAC_SELFTEST_ADDRESS: $MAC_SELFTEST_KEY" >&2
[ "$MAC_SELFTEST_MODE" = no-copy ] && exit 1
exit 0
EOF
chmod +x "$work/bin/ssh" "$work/bin/scp"

failures=0
bad() {
	echo "mac-selftest: $*" >&2
	failures=$((failures + 1))
}
# run <mode> <expected status>: one run, then nothing of the Mac anywhere it printed or wrote.
run() {
	mode=$1
	expected=$2
	rm -rf "$work/out" "$work/tmp" "$work/notes"
	mkdir "$work/out" "$work/tmp"
	: >"$work/notes"
	status=0
	PATH="$work/bin:$PATH" TMPDIR="$work/tmp" MAC_SELFTEST_MODE=$mode MAC_SELFTEST_NOTES="$work/notes" \
		MAC_SELFTEST_ADDRESS=$address MAC_SELFTEST_LOGIN=$login MAC_SELFTEST_KEY=$key \
		MAC_SELFTEST_KNOWN="$work/known/hosts" \
		GO_MAC_SSH="${target-$login@$address:2200}" GO_MAC_KNOWN_HOSTS="${known-$work/known/hosts}" \
		sh "$here/mac.sh" "$work/go-microsip-darwin-amd64" "$work/out" >"$work/stdout" 2>"$work/stderr" || status=$?
	[ "$status" = "$expected" ] || bad "$mode: exit $status, expected $expected: $(cat "$work/stdout" "$work/stderr")"
	[ ! -s "$work/notes" ] || bad "$mode: $(cat "$work/notes")"
	if grep -r -l -e "$address" -e "$login" -e "$key" "$work/stdout" "$work/stderr" "$work/out" "$work/tmp" >/dev/null 2>&1; then
		bad "$mode: the Mac's address, user name or key is in what the run printed or left"
	fi
	[ -z "$(ls -A "$work/tmp")" ] || bad "$mode: the run left temporary files"
}

run ok 0
grep -q '^pass self-test' "$work/stdout" || bad "ok: the self-test line is missing"
grep -q '^mac: macOS 15.8.1, x86_64$' "$work/stdout" || bad "ok: the Mac's version line is missing"
[ "$(cat "$work"/out/*.log)" = "$(cat "$work/stdout")" ] || bad "ok: the retained log is not what was printed"
run unknown-key 1
run password 1
run garbage 1
run garbage-version 1
run failing 1
run no-copy 1
run interrupted 130
target="" run unset-target 2
grep -q GO_MAC_SSH "$work/stderr" || bad "unset-target: the message does not name GO_MAC_SSH"
known="" run unset-known 2
grep -q GO_MAC_KNOWN_HOSTS "$work/stderr" || bad "unset-known: the message does not name GO_MAC_KNOWN_HOSTS"
for target in "$login" "-oProxyCommand=x@$address" "$login@$address:22:22" "$login@$address;id" "$login@-$address"; do
	run "target-syntax" 2
done
unset target
known="$here/mac.sh" run key-file-in-repository 2

if [ "$failures" -gt 0 ]; then
	echo "mac-selftest: $failures control(s) failed" >&2
	exit 1
fi
echo "mac-selftest: the Mac check keeps the address, the user name and the host key to itself"
