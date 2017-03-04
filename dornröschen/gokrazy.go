// +build gokrazy

package main

import (
	"log"
	"time"

	"github.com/gokrazy/gokrazy"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	gokrazy.WaitForClock()

	// Run forever, trigger a run at 10:00 each Monday through Friday.
	for {
		now := time.Now()
		runToday := now.Hour() < 10 &&
			now.Weekday() != time.Saturday &&
			now.Weekday() != time.Sunday
		today := now.Day()
		log.Printf("now = %v, runToday = %v", now, runToday)
		for {
			if time.Now().Day() != today {
				// Day changed, re-evaluate whether to run today.
				break
			}

			nextHour := time.Now().Truncate(time.Hour).Add(1 * time.Hour)
			log.Printf("today = %d, runToday = %v, next hour: %v", today, runToday, nextHour)
			time.Sleep(time.Until(nextHour))

			if time.Now().Hour() >= 10 && runToday {
				runToday = false
				log.Println("Running dornröschen")
				if err := run(); err != nil {
					log.Printf("failed: %v", err)
				}
			}
		}
	}
}
