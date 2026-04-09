#!/bin/sh

TMP_DIR="./tmp"
UPDATER_NAME="updater"
UPDATER_PATH="./$UPDATER_NAME"

mkdir -p "$TMP_DIR"
cp "$UPDATER_PATH" "$TMP_DIR/"

chmod +x "$TMP_DIR/$UPDATER_NAME"

eips 10 10 "Run updater"
"$TMP_DIR/$UPDATER_NAME" eips

rm -rf "$TMP_DIR"
eips 10 10 "Close updater"
