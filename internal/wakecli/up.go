package wakecli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stapelberg/zkj-nas-tools/internal/wake"
)

var upCmd = &cobra.Command{
	Use:   "up <hostname>",
	Short: "Wake up a machine",
	Long:  `Wake up a machine using Wake-on-LAN or MQTT relay.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host, err := lookupHost(args[0])
		if err != nil {
			return err
		}
		return wakeUp(host)
	},
}

// ProgressEvent matches webwake's SSE event format.
type ProgressEvent struct {
	Phase     string `json:"phase"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
}

// Phase represents a wake phase with its display state.
type Phase struct {
	Name    string // "checking", "waking", "ssh", "health", "complete"
	Label   string
	Status  string // "start", "done", "skipped", "error", "already_running"
	Detail  string
	Elapsed time.Duration
}

var phases = []Phase{
	{Name: "checking", Label: "Checking"},
	{Name: "waking", Label: "Waking"},
	{Name: "ssh", Label: "Waiting for SSH"},
	{Name: "health", Label: "Health check"},
}

// Spinner frames for in-progress animation.
var spinnerFrames = []string{"◐", "◓", "◑", "◒"}

// https://en.wikipedia.org/wiki/ANSI_escape_code#Colors
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiGray   = "\033[90m"
)

func colored(color, text string) string {
	return color + text + ansiReset
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func render(target string, phases []Phase, spinnerFrame int, totalElapsed time.Duration, done bool) string {
	var b strings.Builder

	b.WriteString(colored(ansiBold, fmt.Sprintf("wake up %s", target)))
	b.WriteString("\n\n")

	for _, p := range phases {
		var symbol string
		var color string

		switch p.Status {
		case "done":
			symbol = "✓"
			color = ansiGreen
		case "start":
			symbol = spinnerFrames[spinnerFrame%len(spinnerFrames)]
			color = ansiYellow
		case "skipped":
			symbol = "○"
			color = ansiGray
		case "error":
			symbol = "✗"
			color = ansiRed
		default: // pending
			symbol = "○"
			color = ansiGray
		}

		// Format: "  ✓ Checking         0.2s   host is down"
		// Pad label before styling to avoid ANSI codes breaking alignment
		paddedLabel := fmt.Sprintf("%-17s", p.Label)
		elapsed := formatDuration(p.Elapsed)
		paddedElapsed := fmt.Sprintf("%6s", elapsed)
		line := fmt.Sprintf("  %s %s %s", colored(color, symbol), colored(color, paddedLabel), paddedElapsed)
		if p.Detail != "" {
			line += "   " + colored(ansiGray, p.Detail)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if done {
		fmt.Fprintf(&b, "  Total: %s\n", formatDuration(totalElapsed))
	} else {
		fmt.Fprintf(&b, "  Total: %s...\n", formatDuration(totalElapsed))
	}

	return b.String()
}

func clearLines(n int) {
	// Move cursor up n lines and clear each
	for range n {
		fmt.Print("\033[A\033[2K")
	}
}

func wakeUp(target wake.Host) error {
	wakeURL := "http://" + target.Relay + ":8911/wake/stream?machine=" + target.Name

	resp, err := http.Get(wakeURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %v", resp.Status)
	}

	phasesCopy := slices.Clone(phases)
	startTime := time.Now()
	spinnerFrame := 0

	output := render(target.Name, phasesCopy, spinnerFrame, 0, false)
	printedLines := strings.Count(output, "\n")
	fmt.Print(output)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	events := make(chan ProgressEvent)
	done := make(chan error, 1)

	// Read SSE events in background
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var event ProgressEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			events <- event
		}
		done <- scanner.Err()
	}()

	rerender := func(dur time.Duration) {
		clearLines(printedLines)
		output := render(target.Name, phasesCopy, spinnerFrame, dur, true)
		printedLines = strings.Count(output, "\n")
		fmt.Print(output)
	}
	for {
		select {
		case event := <-events:
			for i := range phasesCopy {
				if phasesCopy[i].Name != event.Phase {
					continue
				}
				phasesCopy[i].Status = event.Status
				phasesCopy[i].Detail = event.Detail
				if event.ElapsedMs > 0 {
					phasesCopy[i].Elapsed = time.Duration(event.ElapsedMs) * time.Millisecond
				}
				break
			}

			if event.Phase == "complete" {
				rerender(time.Duration(event.ElapsedMs) * time.Millisecond)
				if event.Status == "error" {
					return fmt.Errorf("%s", event.Detail)
				}
				return nil
			}

			rerender(time.Since(startTime))

		case <-ticker.C:
			spinnerFrame++
			rerender(time.Since(startTime))

		case err := <-done:
			return err
		}
	}
}

func printError(err error) {
	fmt.Fprintf(os.Stderr, "\n%s\n", colored(ansiRed, "Error: "+err.Error()))
}
