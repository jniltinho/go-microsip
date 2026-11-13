#!/bin/sh
# Checks of the macOS build on a Mac reached over SSH (add-macos-universal, design D10).
#   GO_MAC_SSH=user@host[:port] GO_MAC_KNOWN_HOSTS=/path/known_hosts build/vm/mac.sh <file> [out dir]
#
# <file> is the macOS zip, or one executable built for the Mac's architecture. The Mac is named
# by the environment alone and its host key by a file the operator made and verified, kept outside
# the repository. Only public-key authentication is tried: a Mac that wants a password, or shows
# another key, fails the run, and nothing is ever asked.
#
# Nothing the Mac or ssh says is printed or kept: it goes to a temporary folder that is removed
# however the run ends. What is printed, and written to <out dir>/<time>.log (git-ignored
# artifacts/go-mac by default), is this script's own lines, and of the Mac only values that match
# a pattern: the macOS version, the architecture, the version line of the program.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd -P)
[ $# -ge 1 ] || {
	echo "usage: GO_MAC_SSH=user@host[:port] GO_MAC_KNOWN_HOSTS=file build/vm/mac.sh <zip or executable> [out dir]" >&2
	exit 2
}
file=$1
out=${2:-$root/artifacts/go-mac}
target=${GO_MAC_SSH:-}
known=${GO_MAC_KNOWN_HOSTS:-}
[ -n "$target" ] || {
	echo "mac: set GO_MAC_SSH to the Mac, as user@host or user@host:port" >&2
	exit 2
}
[ -n "$known" ] || {
	echo "mac: set GO_MAC_KNOWN_HOSTS to a host-key file you made and verified, outside the repository" >&2
	exit 2
}
[ -f "$file" ] || {
	echo "mac: the file to check does not exist" >&2
	exit 2
}
name=$(basename "$file")
# The name is used in commands on the Mac: plain characters only.
case $name in *[!A-Za-z0-9._-]* | -* | headless.sh)
	echo "mac: the file's name must be made of letters, digits, dots, dashes and underscores" >&2
	exit 2
	;;
esac

# The target is data: it is split here and handed to ssh as arguments of its own, never as text
# a shell or ssh could read options from. shortcut: no IPv6 literal, add it when a Mac needs one.
case $target in
*[!A-Za-z0-9@._:-]* | -* | *@*@* | *:*:*) target="" ;;
*@*) ;;
*) target="" ;;
esac
user=${target%%@*}
host=${target#*@}
port=22
case $host in *:*)
	port=${host##*:}
	host=${host%%:*}
	;;
esac
case $port in '' | *[!0-9]*) target="" ;; esac
case $host in '' | -* | .*) target="" ;; esac
[ -n "$target" ] && [ -n "$user" ] || {
	echo "mac: GO_MAC_SSH is not user@host or user@host:port" >&2
	exit 2
}
[ -f "$known" ] || {
	echo "mac: GO_MAC_KNOWN_HOSTS does not name a file" >&2
	exit 2
}
known=$(cd "$(dirname "$known")" && pwd -P)/$(basename "$known")
case $known in "$root"/*)
	echo "mac: keep the host-key file outside the repository" >&2
	exit 2
	;;
esac

tmp=$(mktemp -d)
there=""
# What this run left on the Mac and here goes with it, however it ends.
cleanup() {
	code=$?
	case $there in /tmp/go-microsip.*) remote "rm -rf $there" || : ;; esac
	rm -rf "$tmp"
	exit "$code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Every connection: the operator's key file alone, public-key authentication alone, no question.
connect() {
	tool=$1
	flag=$2
	shift 2
	"$tool" "$flag" "$port" -o BatchMode=yes -o StrictHostKeyChecking=yes \
		-o "UserKnownHostsFile=$known" -o GlobalKnownHostsFile=/dev/null \
		-o PreferredAuthentications=publickey -o PasswordAuthentication=no \
		-o KbdInteractiveAuthentication=no -o ConnectTimeout=15 -o LogLevel=ERROR \
		"$@" >"$tmp/out" 2>"$tmp/err"
}
remote() { connect ssh -p "$user@$host" "$1"; }
send() { connect scp -P "$@" "$user@$host:$there/"; }

mkdir -p "$out"
log=$out/$(date -u +%Y%m%dT%H%M%SZ).log
say() {
	printf '%s\n' "$*"
	printf '%s\n' "$*" >>"$log"
}
# The Mac's answer to a question this script asked, or nothing unless all of it matches the
# pattern. Whatever else the Mac's shell printed around it is not looked at.
value() { sed -n "s/^$1=//p" "$tmp/out" | head -n 1 | grep -E -x -- "$2" || :; }

failed=0
check() {
	name=$1
	shift
	if "$@"; then say "pass $name"; else
		say "FAIL $name"
		failed=1
	fi
}

say "file: $(sha256sum "$file" | cut -d' ' -f1)  $name"
if ! remote 'echo macos=$(sw_vers -productVersion); echo arch=$(uname -m); echo folder=$(mktemp -d /tmp/go-microsip.XXXXXX)'; then
	say "FAIL connect: no connection, a host key that is not in the file, or no accepted key"
	exit 1
fi
macos=$(value macos '[0-9]+(\.[0-9]+){1,2}')
arch=$(value arch 'x86_64|arm64')
there=$(value folder '/tmp/go-microsip\.[A-Za-z0-9]+')
if [ -z "$macos" ] || [ -z "$arch" ] || [ -z "$there" ]; then
	say "FAIL connect: the Mac's answer is not a macOS version, an architecture and a folder"
	exit 1
fi
say "mac: macOS $macos, $arch"

if ! send "$file" "$root/build/qa/headless.sh"; then
	say "FAIL copy"
	exit 1
fi
case $name in
*.zip)
	bin=$there/go-microsip.app/Contents/MacOS/go-microsip
	check "unpack" remote "cd $there && unzip -q -o $name && test -x $bin"
	;;
*)
	bin=$there/$name
	check "unpack" remote "chmod 700 $bin && test -x $bin"
	;;
esac
[ "$failed" = 0 ] || exit 1

if remote "$bin version"; then
	version=$(grep -E -x -m 1 -- 'go-microsip [0-9A-Za-z.+-]+ \([0-9A-Za-z]+\)' "$tmp/out" || :)
	if [ -n "$version" ]; then say "pass version: $version"; else
		say "FAIL version: not a version line"
		failed=1
	fi
else
	say "FAIL version"
	failed=1
fi
check "headless (version, doctor, MCP handshake, password-argument refusal, configuration)" \
	remote "sh $there/headless.sh $bin"
# The window opens and the page calls the backend; two minutes at most.
check "self-test (the window opens and the page calls the backend)" \
	remote "perl -e 'alarm shift; exec @ARGV' 120 $bin --self-test"
[ "$failed" = 0 ] && say "mac: all checks passed" || say "mac: a check failed"
exit "$failed"
