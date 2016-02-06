The HMServer process on my HomeMatic CCU2 dies every other week because of
memory pressure. Since eq-3 (the vendor) does not even reply to my bug reports,
this script just reboots the device.

To install, use:

```
midna $ scp watchdog.sh root@homematic-ccu2:/usr/local/zkj-watchdog.sh

homematic-ccu2 # echo '* * * * * /usr/local/zkj-watchdog.sh' >> /usr/local/crontabs/root
homematic-ccu2 # reboot
```

(the reboot is for crond to pick up the changes)
