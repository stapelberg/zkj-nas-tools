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

	_ "net/http/pprof"
)

var (
	listen = flag.String("listen",
		":5555",
		"[host]:port to listen on.")

	midnaURL = flag.String("midna_url",
		"http://midna:4000/",
		"URL on which runstatus.go is accessible")

	subwooferLevel = map[string]int{
		"MPLAY": 50, // PC
		"BD":    38, // beast
		"GAME":  38, // chromecast
		"AUX1":  50, // chromecast audio
	}

	volume = map[string]int{
		"MPLAY": 60, // PC
		"BD":    60, // beast
		"GAME":  60, // chromecast
		"AUX1":  60, // chromecast audio
	}
)

type State struct {
	chromecastPlaying      bool
	chromecastAudioPlaying bool
	beastPowered           bool
	midnaUnlocked          bool
	avrPowered             bool
	roombaCanClean         bool
	roombaCleaning         bool
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

	next.avrPowered = current.chromecastAudioPlaying || current.beastPowered || current.midnaUnlocked
	// next.difmxChannel = 0 // midna
	// if current.beastPowered {
	// 	next.difmxChannel = 1 // beast
	// }
	// Cleaning is okay between 10:15 and 13:00 on work days
	now := time.Now()
	hour, minute := now.Hour(), now.Minute()
	next.roombaCanClean = now.Weekday() != time.Saturday &&
		now.Weekday() != time.Sunday &&
		((hour == 10 && minute > 15) || hour == 11 || hour == 12)
	// Override: don’t clean if someone is at home
	if next.avrPowered {
		next.roombaCanClean = false
	}
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

	go discoverAndPollChromecasts()
	go pingBeast()
	go pollMidna()
	go scheduleRoomba()
	//go pollDifmx()

	// Wait a little bit to give the various goroutines time to do their initial polls.
	time.Sleep(10 * time.Second)

	for {
		stateChanged.L.Lock()
		stateChanged.Wait()

		stateMu.RLock()
		log.Printf("determining outputs based on %+v\n", state)
		next := stateMachine(state)
		log.Printf("syncing outputs, next = %+v\n", next)
		if state.avrPowered != next.avrPowered {
			var avrCmd string
			if next.avrPowered {
				avrCmd = "on"
			} else {
				alwaysOff := true
				for _, s := range stateHistory {
					// If 60 seconds haven’t even passed or the AVR was
					// supposed to be turned on at some point, don’t turn it
					// off yet.
					if s.timestamp.IsZero() || s.avrPowered {
						alwaysOff = false
						break
					}
				}
				if alwaysOff {
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
				} else {
					if got, want := resp.StatusCode, http.StatusOK; got != want {
						log.Printf("unexpected HTTP status code: got %v, want %v", got, want)
					} else {
						stateMu.RUnlock()
						stateMu.Lock()
						state.avrPowered = avrCmd == "on"
						stateMu.Unlock()
						stateMu.RLock()
					}
				}
			}
		}

		// if state.difmxChannel != next.difmxChannel {
		// 	if err := switchDifmxChannel(next.difmxChannel); err != nil {
		// 		log.Printf("switchDifmxChannel: %v", err)
		// 	}
		// }

		if next.roombaCanClean && roombaLastClean.YearDay() != time.Now().YearDay() {
			roombaLastClean = time.Now()
			log.Printf("Instructing Roomba to clean")
			select {
			case toRoomba <- "start":
			default:
			}
		}
		if !next.roombaCanClean && state.roombaCleaning {
			log.Printf("Instructing Roomba to return to dock")
			select {
			case toRoomba <- "dock":
			default:
			}
		}

		nextHistoryEntry := stateHistory[(stateHistoryPos+1)%len(stateHistory)]
		keep := time.Since(nextHistoryEntry.timestamp) >= 60*time.Second
		if nextHistoryEntry.timestamp.IsZero() {
			keep = time.Since(stateHistory[stateHistoryPos-1].timestamp) >= 1*time.Second
		}
		stateMu.RUnlock()
		if keep {
			stateMu.Lock()
			next.timestamp = time.Now()
			stateHistory[stateHistoryPos] = next
			stateHistoryPos = (stateHistoryPos + 1) % len(stateHistory)
			stateMu.Unlock()
		}

		stateChanged.L.Unlock()
	}
}
