#!/bin/zsh
set -e
LABEL="com.newapi.local"
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
echo "New API stopped."
