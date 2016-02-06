#!/bin/sh

if [ $(sed 's/\..*$//g' /proc/uptime) -lt 600 ]
then
  echo "Running for less than 10 minutes, system might not have booted up yet."
  exit 0
fi

if ! netstat -ltn 2>&- | grep -q '\b0.0.0.0:9292\b'
then
  echo "Not listening on :9292 anymore, rebooting."
  reboot
fi
