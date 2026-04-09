#!/bin/sh

LOGFILE="/mnt/us/extensions/KindleTeleSync/sync.log"
BINARY="/mnt/us/extensions/KindleTeleSync/kindle_sync_d"
MAXSIZE=$((5 * 1024 * 1024)) # 5 MB

eips 30 0 "Start telesync..."
#Clean logs if it > MAXSIZE
if [ -f "$LOGFILE" ]; then
    actual_size=$(stat -c%s "$LOGFILE")
    if [ "$actual_size" -gt "$MAXSIZE" ]; then
        rm "$LOGFILE"
    fi
fi

if [ -x "$BINARY" ]; then
    mkdir -p /mnt/us/books
    "$BINARY" >> "$LOGFILE" 2>&1
else
    echo "[$(date)] Ошибка: бинарник не найден или не исполняемый." >> "$LOGFILE"
fi
sleep 10
eips 30 0 "                             "
eips 0 3 "                                                          "