package main

//go:generate protoc --go_out=import_path=cast_channel:. cast_channel/cast_channel.proto

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stapelberg/zkj-nas-tools/internal/timestamped"

	_ "net/http/pprof"
)

var (
	listen = flag.String("listen",
		":5555",
		"[host]:port to listen on.")

	midnaURL = flag.String("midna_url",
		"http://midna:4000/",
		"URL on which runstatus.go is accessible")
)

type State struct {
	beastPowered   bool
	midnaUnlocked  timestamped.Bool
	avrPowered     timestamped.Bool
	roombaCanClean timestamped.Bool
	roombaCleaning bool
	galaxyActive   timestamped.Bool
	iphoneActive   timestamped.Bool
	//difmxChannel           int
	timestamp time.Time
}

var (
	state           State
	lastContact     = make(map[string]time.Time)
	roombaLastClean time.Time
	// stateHistory stores the “next” state (output of stateMachine()), as
	// calculated over the last 60s. This is used for hysteresis, i.e. not
	// turning off the AVR/video projector immediately when input is gone.
	stateHistory    [60]State
	stateHistoryPos = int(1)
	stateMu         sync.RWMutex

	stateChanged = sync.NewCond(&sync.Mutex{})
)

func stateMachine(current State) State {
	var next State

	// next.difmxChannel = 0 // midna
	// if current.beastPowered {
	// 	next.difmxChannel = 1 // beast
	// }
	now := time.Now()
	weekday, hour, minute := now.Weekday(), now.Hour(), now.Minute()
	anyoneHome :=
		current.midnaUnlocked.Value() ||
			current.beastPowered ||
			(hour > 8 && (current.galaxyActive.Value() || current.iphoneActive.Value()))
	// Cleaning is okay between 10:15 and 13:00 on work days,
	// unless anyone is home.
	next.roombaCanClean.Set(
		weekday != time.Saturday &&
			weekday != time.Sunday &&
			((hour == 10 && minute > 15) || hour == 11 || hour == 12) &&
			!anyoneHome)
	next.avrPowered.Set(anyoneHome)
	return next
}

func main() {
	flag.Parse()

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		stateMu.RLock()
		defer stateMu.RUnlock()
		for target, last := range lastContact {
			fmt.Fprintf(w, "last contact with %q: %v (%v)\n", target, last, time.Since(last))
		}
		fmt.Fprintf(w, "current: %+v\n", state)
		fmt.Fprintf(w, "next: %+v\n", stateMachine(state))
		fmt.Fprintf(w, "\n")
		for i, s := range stateHistory {
			arrow := ""
			if i == stateHistoryPos {
				arrow = "--> "
			}
			fmt.Fprintf(w, "%s%02d: %s avr: %v\n",
				arrow, i, s.timestamp.Format("2006-01-02 15:04:05"), s.avrPowered)
		}
		// TODO: tail log
	})

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	srv := http.Server{Addr: *listen}
	go srv.Serve(ln)

	//go discoverAndPollChromecasts()
	go pingBeast()
	go pollMidna()
	go scheduleRoomba()
	//go pollDifmx()
	go pollAVR()
	go pollDHCP()

	// Wait a little bit to give the various goroutines time to do their initial polls.
	time.Sleep(10 * time.Second)

	for {
		stateChanged.L.Lock()
		stateChanged.Wait()

		stateMu.RLock()
		//fmt.Fprintln(os.Stderr)
		//log.Printf("current state: %+v\n", state)
		desired := stateMachine(state)
		//log.Printf("desired state: %+v\n", desired)
		if state.avrPowered.Value() != desired.avrPowered.Value() {
			var avrCmd string
			if desired.avrPowered.Value() {
				avrCmd = "on"
			} else {
				if time.Since(state.avrPowered.LastChange()) > 10*time.Minute {
					avrCmd = "off"
				} else {
					log.Printf("Not turning AVR off yet (hysteresis).\n")
				}
			}
			if avrCmd != "" {
				log.Printf("Powering %s AVR", avrCmd)
				resp, err := http.Get("http://localhost:8012/power/" + avrCmd)
				if err != nil {
					log.Println(err)
				} else if got, want := resp.StatusCode, http.StatusOK; got != want {
					log.Printf("unexpected HTTP status code: got %v, want %v", got, want)
				}
			}
		}

		// if state.difmxChannel != next.difmxChannel {
		// 	if err := switchDifmxChannel(next.difmxChannel); err != nil {
		// 		log.Printf("switchDifmxChannel: %v", err)
		// 	}
		// }

		if desired.roombaCanClean.Value() &&
			time.Since(state.roombaCanClean.LastChange()) > 5*time.Minute &&
			roombaLastClean.YearDay() != time.Now().YearDay() &&
			time.Now().Weekday() != time.Tuesday {
			roombaLastClean = time.Now()
			log.Printf("Instructing Roomba to clean")
			select {
			case toRoomba <- "start":
			default:
			}
		}
		// Commented out: if humans trigger a roomba cleaning, we shouldn’t interfere
		// if !desired.roombaCanClean.Value() && state.roombaCleaning {
		// 	log.Printf("Instructing Roomba to return to dock")
		// 	select {
		// 	case toRoomba <- "dock":
		// 	default:
		// 	}
		// }

		nextHistoryEntry := stateHistory[(stateHistoryPos+1)%len(stateHistory)]
		keep := time.Since(nextHistoryEntry.timestamp) >= 60*time.Second
		if nextHistoryEntry.timestamp.IsZero() {
			keep = time.Since(stateHistory[stateHistoryPos-1].timestamp) >= 1*time.Second
		}
		stateMu.RUnlock()
		if keep {
			stateMu.Lock()
			desired.timestamp = time.Now()
			stateHistory[stateHistoryPos] = desired
			stateHistoryPos = (stateHistoryPos + 1) % len(stateHistory)
			stateMu.Unlock()
		}

		stateChanged.L.Unlock()
	}
}
