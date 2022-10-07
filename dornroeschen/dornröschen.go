// dornröschen wakes up machines and NASen and backs up data/syncs NAS contents.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/stapelberg/zkj-nas-tools/internal/wake"
	"github.com/stapelberg/zkj-nas-tools/internal/wakeonlan"
)

var (
	runBackup = flag.Bool("backup",
		true,
		"Backup all -backup_hosts? See also -sync")
	runSync = flag.Bool("sync",
		true,
		"Sync all -storage_hosts? See also -backup")
	backupHosts = flag.String("backup_hosts",
		// 100.84.178.54 is exo1
		"midna/38:60:77:ab:d3:ea,eris.noname-ev.de/,ex622.zekjur.net/,100.84.178.54/",
		"Comma-separated list of hosts to back up, each entry is host/mac-address")
	storageHosts = flag.String("storage_hosts",
		"10.0.0.252/d0:50:99:9a:0f:4a,10.0.0.253/70:85:c2:b6:02:24",
		"Comma-separated list of NASen, each entry is host/mac-address")
	backupPrivateKeyPath = flag.String("ssh_backup_private_key_path",
		"/perm/id_ed25519_backup",
		"Path to the SSH private key file to authenticate with at -backup_hosts for backing up")
	suspendPrivateKeyPath = flag.String("ssh_suspend_private_key_path",
		"/perm/id_ed25519_suspend",
		"Path to the SSH private key file to authenticate with at -backup_hosts for suspending to RAM")
	syncPrivateKeyPath = flag.String("ssh_sync_private_key_path",
		"/perm/id_rsa_sync",
		"Path to the SSH private key file to authenticate with at -storage_hosts for syncing")

	mqttBroker = flag.String("mqtt_broker",
		"tcp://dr.lan:1883",
		"MQTT broker address for github.com/eclipse/paho.mqtt.golang")
)

func splitHostMAC(hostmac string) (host, mac string) {
	parts := strings.Split(hostmac, "/")
	if len(parts) != 2 {
		log.Fatalf(`"%s" is not in format host/MAC`, hostmac)
	}
	return parts[0], parts[1]
}

func wakeUp(host, mac string) (woken bool, _ error) {
	ctx := context.Background()

	{
		ctx, canc := context.WithTimeout(ctx, 1*time.Minute)
		defer canc()
		if err := wake.PollSSH1(ctx, host+":22"); err == nil {
			return false, nil // already up and running
		}
	}

	if host == "10.0.0.252" {
		// push the mainboard power button to turn off the PC part (ESP32 will
		// keep running on USB +5V standby power).
		log.Printf("pushing storage2 mainboard power button")
		const clientID = "https://github.com/stapelberg/zkj-nas-tools/dornroeschen"
		if err := wake.PushMainboardPower(*mqttBroker, clientID); err != nil {
			log.Printf("pushing storage2 mainboard power button failed: %v", err)
		}
	} else {
		if err := wakeonlan.SendMagicPacket(nil, mac); err != nil {
			log.Printf("sendWOL: %v", err)
		} else {
			log.Printf("Sent magic packet to %v", mac)
		}
	}

	{
		ctx, canc := context.WithTimeout(ctx, 5*time.Minute)
		defer canc()
		if err := wake.PollSSH(ctx, host+":22"); err != nil {
			return true, err
		}
	}

	log.Printf("NAS %s now reachable via SSH", host)

	return true, nil
}

func dramaqueenRequest(NAS, lock, method string) error {
	retry := 0
	for retry < 5 {
		retry++
		resp, err := http.Post("http://"+NAS+":4414/"+method+"?key="+lock, "text/plain", nil)
		if err != nil {
			if retry == 5 {
				return fmt.Errorf(`Could not acquire dramaqueen lock on %s: %v`, NAS, err)
			} else {
				log.Printf(`Could not acquire dramaqueen lock on %s: %v`, NAS, err)
				time.Sleep(time.Duration(math.Pow(2, float64(retry))) * time.Second)
				continue
			}
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf(`dramaqueen request on %s resulted in HTTP %d`, NAS, resp.StatusCode)
		}
		log.Printf("dramaqueen lock request succeeded: lock=%s method=%s", lock, method)
		break
	}
	return nil
}

func lockDramaqueen(NAS, lock string) error {
	// TODO: dramaqueen should return an error if the lock already exists so that overruns will fail.
	return dramaqueenRequest(NAS, lock, "inhibit")
}

func releaseDramaqueenLock(NAS, lock string) error {
	return dramaqueenRequest(NAS, lock, "release")
}

func backup(dest string) bool {
	log.Printf("Backup destination is %s", dest)
	destHost, destMAC := splitHostMAC(dest)

	wokenNAS, err := wakeUp(destHost, destMAC)
	if err != nil {
		log.Fatalf("Could not wake up NAS %s: %v", destHost, err)
	}

	time.Sleep(10 * time.Second) // to finish boot

	for _, source := range strings.Split(*backupHosts, ",") {
		sourceHost, sourceMAC := splitHostMAC(source)

		// Prevent dramaqueen on the destination NAS from shutting it down. If
		// the dramaqueen lock cannot be acquired, just continue and hope for
		// the best (in case a NAS is not running dramaqueen, it won’t shut
		// down automatically anyway).
		lockname := "backup-" + sourceHost
		if err := lockDramaqueen(destHost, lockname); err == nil {
			defer releaseDramaqueenLock(destHost, lockname)
		}

		woken := false
		if sourceMAC != "" {
			var err error
			woken, err = wakeUp(sourceHost, sourceMAC)
			if err != nil {
				log.Printf("Backup of %s failed: %v", sourceHost, err)
				continue
			}
		}

		// The command is just destHost, because for the SSH key this program
		// is using, the remote host will only ever run /root/backup.pl, which
		// interprets the command as the destination host.
		outputfile, err := sshCommand(sourceHost, *backupPrivateKeyPath, destHost)
		// Dump the output into the log, which is persisted via remote syslog:
		if b, err := ioutil.ReadFile(outputfile); err == nil {
			log.Printf("[%s] SSH output", sourceHost)
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				log.Printf("[%s]   %s", sourceHost, line)
			}
			log.Printf("[%s] End of SSH output", sourceHost)
		}
		if err != nil {
			log.Printf("Backup of %s failed: %v\n", sourceHost, err)
			continue
		}

		// Suspend the machine to RAM, but only if we have woken it up.
		if !woken {
			continue
		}

		if _, err := sshCommand(sourceHost, *suspendPrivateKeyPath, ""); err != nil {
			log.Printf("Suspending %s to RAM failed: %v", sourceHost, err)
		}
	}

	return wokenNAS
}

func suspendNAS(destHost string) {
	log.Printf("suspending NAS %s", destHost)
	if _, err := sshCommand(destHost, *suspendPrivateKeyPath, ""); err != nil {
		log.Printf("Suspending %s failed: %v", destHost, err)
	}
}

func sync(NASen []string) {
	for _, dest := range NASen {
		destHost, destMAC := splitHostMAC(dest)
		woken, err := wakeUp(destHost, destMAC)
		if err != nil {
			log.Fatalf("Could not wake up NAS %s\n", destHost)
		}
		if woken {
			defer suspendNAS(destHost)
		}
		lockDramaqueen(destHost, "sync")
	}

	for idx, source := range NASen {
		dest := NASen[(idx+1)%len(NASen)]
		sourceHost, _ := splitHostMAC(source)
		destHost, _ := splitHostMAC(dest)
		log.Printf("Syncing %s to %s", sourceHost, destHost)

		outputfile, err := sshCommand(sourceHost, *syncPrivateKeyPath, destHost)
		if err != nil {
			log.Printf("Syncing of %s to %s failed: %v", sourceHost, destHost, err)
		}
		log.Printf("sync %s to %s output stored in %s", sourceHost, destHost, outputfile)
	}

	for _, dest := range NASen {
		destHost, _ := splitHostMAC(dest)
		// With the lock released, the NASen will turn off on their own (unless
		// somebody is using them, of course).
		releaseDramaqueenLock(destHost, "sync")
	}
}

func run() error {
	if !*runBackup && !*runSync {
		return fmt.Errorf("Neither -backup nor -sync enabled, nothing to do.")
	}

	storageList := strings.Split(*storageHosts, ",")
	if len(storageList) > 2 {
		return fmt.Errorf("More than 2 -storage_hosts are not supported. Please send a patch to fix.")
	}

	// Alternate between the available NASen to make sure each one works.
	dest := storageList[(time.Now().Day()+1)%len(storageList)]

	wokenNAS := false
	if *runBackup {
		wokenNAS = backup(dest)
	}

	if *runSync {
		sync(storageList)
	}

	if wokenNAS {
		destHost, _ := splitHostMAC(dest)
		suspendNAS(destHost)
	}

	return nil
}
