#!/bin/sh
# The call notifier against a notification service (run inside the build container): the test is
# itself the service of a private session bus; then a service that left and that the bus starts
# again; then a bus without one, and no bus at all.
#   build/qa/notify.sh <test binary>
set -eu
test_binary=$1
HOME=$(mktemp -d)
XDG_RUNTIME_DIR=$(mktemp -d)
export HOME XDG_RUNTIME_DIR
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
dbus-run-session --config-file="$bare" -- env GO_MICROSIP_NOTIFY_TEST=bus "$test_binary" -test.v -test.run "TestCallNotificationsOnASessionBus|TestCallerTextIsPlain"
echo "notify: the notifier passed against a notification service"
# A bus that can start the notification service: the test binary itself, in that part.
services=$(mktemp -d)
seen=$(mktemp)
cat >"$services/org.freedesktop.Notifications.service" <<SERVICE
[D-BUS Service]
Name=org.freedesktop.Notifications
Exec=/usr/bin/env GO_MICROSIP_NOTIFY_TEST=service GO_MICROSIP_NOTIFY_SEEN=$seen $test_binary -test.run TestTheActivatedNotificationService
SERVICE
starting=$(mktemp)
sed "s|</busconfig>|  <servicedir>$services</servicedir></busconfig>|" "$bare" >"$starting"
dbus-run-session --config-file="$starting" -- env GO_MICROSIP_NOTIFY_TEST=activation GO_MICROSIP_NOTIFY_SEEN="$seen" "$test_binary" -test.v -test.run "TestANotificationServiceThatLeftIsStartedAgain"
echo "notify: a notification service that left is started again for the next call"
dbus-run-session --config-file="$bare" -- env GO_MICROSIP_NOTIFY_TEST=none "$test_binary" -test.v -test.run "TestNoNotificationServiceIsQuiet"
echo "notify: a session bus without a notification service is quiet"
DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/bus GO_MICROSIP_NOTIFY_TEST=none "$test_binary" -test.v -test.run "TestNoNotificationServiceIsQuiet"
echo "notify: a session without a bus is quiet"
