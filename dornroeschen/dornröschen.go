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
	"os"
	"strings"
	"time"

	"github.com/stapelberg/zkj-nas-tools/internal/wake"
	"golang.org/x/sync/errgroup"
)

var (
	runBackup = flag.Bool("backup",
		true,
		"Backup all -backup_hosts? See also -sync")
	runSync = flag.Bool("sync",
		true,
		"Sync all -storage_hosts? See also -backup")
	backupHosts = flag.String("backup_hosts",
		"midna/38:60:77:ab:d3:ea,ex622.zekjur.net/,exo1/,zammad/",
		"Comma-separated list of hosts to back up, each entry is host/mac-address")
	storageHosts = flag.String("storage_hosts",
		"10.0.0.252/d0:50:99:9a:0f:4a,10.0.0.253/70:85:c2:8d:b9:76",
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
		log.Fatalf("%q is not in format host/MAC", hostmac)
	}
	return parts[0], parts[1]
}

func wakeUp(host, mac string) (woken bool, _ error) {
	cfg := wake.Config{
		MQTTBroker: *mqttBroker,
		ClientID:   "https://github.com/stapelberg/zkj-nas-tools/dornroeschen",
		Host:       host,
		IP:         host,
		MAC:        mac,
	}
	err := cfg.Wakeup(context.Background())
	if err == wake.ErrAlreadyRunning {
		return false, nil // already up and running
	}
	if err != nil {
		return true, err // tried to wake up, but failed
	}
	return true, nil // successfully woken up
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

func backup1(destHost, sourceHost, sourceMAC string) error {
	log := log.New(os.Stderr, sourceHost+" ", log.LstdFlags)

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
			return fmt.Errorf("backup of %s failed: %v", sourceHost, err)
		}
	}

	// The command is just destHost, because for the SSH key this program
	// is using, the remote host will only ever run /root/backup.pl, which
	// interprets the command as the destination host.
	outputfile, err := rsyncSSH(sourceHost, *backupPrivateKeyPath, destHost)
	// Dump the output into the log, which is persisted via remote syslog:
	if b, err := ioutil.ReadFile(outputfile); err == nil {
		log.Println("SSH output")
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			log.Println("   " + line)
		}
		log.Println("End of SSH output")
	}
	if err != nil {
		return fmt.Errorf("backup of %s failed: %v\n", sourceHost, err)
	}

	// Suspend the machine to RAM, but only if we have woken it up.
	// midna suspends if pacna is running.
	suspend := woken || sourceHost == "midna"
	if !suspend {
		return nil
	}

	suspendNAS(sourceHost)
	return nil
}

func backup(dest string, sources []string) (woken bool, _ error) {
	log.Printf("Backup destination is %s", dest)
	destHost, destMAC := splitHostMAC(dest)

	wokenNAS, err := wakeUp(destHost, destMAC)
	if err != nil {
		return false, fmt.Errorf("Could not wake up NAS %s: %v", destHost, err)
	}

	// Just in case dramaqueen needs some extra time to start up.
	time.Sleep(60 * time.Second)

	// Run all backups in parallel
	var eg errgroup.Group
	for _, source := range sources {
		sourceHost, sourceMAC := splitHostMAC(source)
		eg.Go(func() error {
			if err := backup1(destHost, sourceHost, sourceMAC); err != nil {
				log.Print(err)
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return wokenNAS, err
	}

	return wokenNAS, nil
}

func suspendNAS(destHost string) {
	log.Printf("suspending NAS %s", destHost)
	if err := sshCommand(log.Default(), destHost, *suspendPrivateKeyPath, ""); err != nil {
		log.Printf("Suspending %s failed: %v", destHost, err)
	}
}

func sync(NASen []string) error {
	for _, dest := range NASen {
		destHost, destMAC := splitHostMAC(dest)
		woken, err := wakeUp(destHost, destMAC)
		if err != nil {
			return fmt.Errorf("Could not wake up NAS %s\n", destHost)
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

		outputfile, err := rsyncSSH(sourceHost, *syncPrivateKeyPath, destHost)
		if err != nil {
			log.Printf("Syncing of %s to %s failed: %v", sourceHost, destHost, err)
		}
		log.Printf("sync %s to %s output stored in %s", sourceHost, destHost, outputfile)

		// Dump the output into the log, which is persisted via remote syslog:
		if b, err := ioutil.ReadFile(outputfile); err == nil {
			log.Println("SSH output")
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				log.Println("   " + line)
			}
			log.Println("End of SSH output")
		}
	}

	for _, dest := range NASen {
		destHost, _ := splitHostMAC(dest)
		// With the lock released, the NASen will turn off on their own (unless
		// somebody is using them, of course).
		releaseDramaqueenLock(destHost, "sync")
	}

	return nil
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

	var firstErr error
	wokenNAS := false
	if *runBackup {
		var err error
		wokenNAS, err = backup(dest, strings.Split(*backupHosts, ","))
		if err != nil {
			log.Printf("backup: %v", err)
			if firstErr == nil {
				firstErr = err
				// Keep going: sync() should run even if backup() fails
			}
		}
	}

	if *runSync {
		if err := sync(storageList); err != nil {
			log.Printf("sync: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if wokenNAS {
		destHost, _ := splitHostMAC(dest)
		suspendNAS(destHost)
	}

	return firstErr
}

func reachableViaSSH(host string) bool {
	ctx, canc := context.WithTimeout(context.Background(), 5*time.Second)
	defer canc()
	return wake.PollSSH1(ctx, host+":22") == nil
}

func runOpportunisticBackups1(host string) {
	var startBackup time.Time
	prevReachable := false
	tick := time.Tick(5 * time.Minute)
	for range tick {
		nowReachable := reachableViaSSH(host)
		log.Printf("[%s] opportunistic backup check; reachable=%v", host, nowReachable)
		if !prevReachable && nowReachable {
			startBackup = time.Now().Add(15 * time.Minute)
			log.Printf("[%s] became reachable, starting backup in %v", host, startBackup)
		} else if prevReachable && !nowReachable {
			log.Printf("[%s] became unreachable", host)
			startBackup = time.Time{}
		} else if prevReachable && nowReachable && time.Now().After(startBackup) && !startBackup.IsZero() {
			startBackup = time.Time{}

			storageList := strings.Split(*storageHosts, ",")
			if len(storageList) > 2 {
				log.Printf("More than 2 -storage_hosts are not supported. Please send a patch to fix.")
				continue
			}

			// Alternate between the available NASen to make sure each one works.
			dest := storageList[(time.Now().Day()+1)%len(storageList)]

			log.Printf("[%s] starting backup to dest=%s", host, dest)

			wokenNAS, err := backup(dest, []string{host + "/"})
			if err != nil {
				log.Printf("backup: %v", err)
			}

			if wokenNAS {
				destHost, _ := splitHostMAC(dest)
				suspendNAS(destHost)
			}
		}
		prevReachable = nowReachable
	}
}

func runOpportunisticBackups(hostsList string) {
	hosts := strings.Split(hostsList, ",")
	for _, hostsPart := range hosts {
		h := strings.TrimSpace(hostsPart)
		if h == "" {
			continue
		}
		log.Printf("keeping track of host %s for opportunistic backup", h)
		go runOpportunisticBackups1(h)
	}
}
