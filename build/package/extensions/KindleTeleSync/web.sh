#!/bin/sh
killall webconfig 2>/dev/null
IP=$(ip route get 1 | awk '{print $7}')
URL="http://$IP:8880"
./webconfig qr "$URL"
./webconfig&
PID=$!
echo $PID > /tmp/webconfig.pid
eips -l
eips 10 10 "Open link:"
eips 10 11  "$URL"
eips -g qr.png -x 350 -y 350

SECONDS=160
eips 10 12 "Auto close in: $SECONDS sec"

(sleep $SECONDS && killall webconfig) &

./fbink -f -s