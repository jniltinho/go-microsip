#!/bin/sh
# The keyring adapter against a real Secret Service (run inside the build container): GNOME
# Keyring on a private session bus, unlocked with a throwaway password, then a bus with no Secret
# Service at all. Nothing of the host's keyring is reachable from the container.
#   build/qa/secrets.sh <test binary>
set -eu
test_binary=$1
HOME=$(mktemp -d)
XDG_RUNTIME_DIR=$(mktemp -d)
XDG_DATA_HOME=$HOME/.local/share
export HOME XDG_RUNTIME_DIR XDG_DATA_HOME test_binary
dbus-run-session -- sh -c '
	# --unlock reads the password of the login keyring, which it creates, from its input.
	printf "throwaway-keyring-password" | gnome-keyring-daemon --unlock --components=secrets >/dev/null 2>&1
	i=0
	until dbus-send --session --print-reply --dest=org.freedesktop.DBus /org/freedesktop/DBus \
		org.freedesktop.DBus.NameHasOwner string:org.freedesktop.secrets 2>/dev/null | grep -q true; do
		i=$((i + 1))
		[ "$i" -lt 50 ] || { echo "secrets: the keyring did not start" >&2; exit 1; }
		sleep 0.2
	done
	# The test that locks the keyring comes last: nothing unlocks it again without a prompt.
	GO_MICROSIP_SECRETS_TEST=keyring "$test_binary" -test.v -test.run "TestKeyringStoresReplacesAndDeletes" &&
		GO_MICROSIP_SECRETS_TEST=keyring "$test_binary" -test.v -test.run "TestALockedKeyringIsReportedAndNotPrompted"
'
echo "secrets: the keyring adapter passed against GNOME Keyring"
# A session bus that starts no service on demand: with GNOME Keyring installed, a standard bus
# would start it at the first call.
bare=$(mktemp)
cat >"$bare" <<'XML'
<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:tmpdir=/tmp</listen>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
XML
dbus-run-session --config-file="$bare" -- env GO_MICROSIP_SECRETS_TEST=none "$test_binary" -test.v -test.run "TestNoKeyringIsReported"
echo "secrets: a session bus without a keyring is reported as no keyring"
DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/bus GO_MICROSIP_SECRETS_TEST=none "$test_binary" -test.v -test.run "TestNoKeyringIsReported"
echo "secrets: a session without a bus is reported as no keyring"
