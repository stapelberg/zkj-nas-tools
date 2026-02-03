package wakecli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stapelberg/zkj-nas-tools/internal/wake"
)

var upCmd = &cobra.Command{
	Use:          "up <hostname>",
	Short:        "Wake up a machine",
	Long:         `Wake up a machine using Wake-on-LAN or MQTT relay.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
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

// encryptedPhases are used for LUKS-encrypted hosts that need interactive unlock.
var encryptedPhases = []Phase{
	{Name: "checking", Label: "Checking"},
	{Name: "waking", Label: "Waking"},
	{Name: "initramfs", Label: "Initramfs SSH"},
	{Name: "unlock", Label: "Unlocking"},
	{Name: "system", Label: "System SSH"},
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
	if target.Name == "verkaufg9" {
		return wakeUpWithUnlock(target)
	}
	return wakeUpStream(target)
}

func wakeUpStream(target wake.Host) error {
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

// pollSSHQuiet polls until SSH becomes reachable, without logging.
func pollSSHQuiet(ctx context.Context, addr string) error {
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	for range tick.C {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := wake.PollSSH1(ctx, addr); err != nil {
			continue
		}
		return nil
	}
	return nil
}

// wakeUpWithUnlock handles LUKS-encrypted hosts like verkaufg9 that need
// interactive unlock after initramfs SSH becomes available.
func wakeUpWithUnlock(target wake.Host) error {
	phasesCopy := slices.Clone(encryptedPhases)
	startTime := time.Now()
	spinnerFrame := 0

	output := render(target.Name, phasesCopy, spinnerFrame, 0, false)
	printedLines := strings.Count(output, "\n")
	fmt.Print(output)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	updatePhase := func(name, status, detail string, elapsed time.Duration) {
		for i := range phasesCopy {
			if phasesCopy[i].Name == name {
				phasesCopy[i].Status = status
				phasesCopy[i].Detail = detail
				if elapsed > 0 {
					phasesCopy[i].Elapsed = elapsed
				}
				break
			}
		}
	}

	rerender := func(done bool) {
		clearLines(printedLines)
		output := render(target.Name, phasesCopy, spinnerFrame, time.Since(startTime), done)
		printedLines = strings.Count(output, "\n")
		fmt.Print(output)
	}

	// startTicker launches a goroutine for spinner animation and returns
	// a channel to stop it. Call close() on the returned channel to stop.
	startTicker := func() chan struct{} {
		stop := make(chan struct{})
		go func() {
			for {
				select {
				case <-ticker.C:
					spinnerFrame++
					rerender(false)
				case <-stop:
					return
				}
			}
		}()
		return stop
	}
	stopTicker := startTicker()

	finishWithError := func(err error) error {
		close(stopTicker)
		rerender(true)
		return err
	}

	finish := func() error {
		close(stopTicker)
		rerender(true)
		return nil
	}

	baseURL := "http://" + target.Relay + ":8911"

	// Phase 1: Check if already up (check Tailscale hostname for full system)
	updatePhase("checking", "start", fmt.Sprintf("checking tcp/22 on %s", target.Name), 0)
	rerender(false)
	phaseStart := time.Now()

	checkCtx, checkCanc := context.WithTimeout(context.Background(), 5*time.Second)
	conn, err := (&net.Dialer{}).DialContext(checkCtx, "tcp", target.Name+":22")
	checkCanc()

	if err == nil {
		conn.Close()
		updatePhase("checking", "done", "already running", time.Since(phaseStart))
		rerender(false)
		return finish()
	}
	updatePhase("checking", "done", "host is down", time.Since(phaseStart))
	rerender(false)

	// Phase 2: Send WoL
	updatePhase("waking", "start", "sending wake signal", 0)
	rerender(false)
	phaseStart = time.Now()

	resp, err := http.Get(baseURL + "/wol?machine=" + target.Name)
	if err != nil {
		updatePhase("waking", "error", err.Error(), time.Since(phaseStart))
		return finishWithError(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("wol returned %s", resp.Status)
		updatePhase("waking", "error", err.Error(), time.Since(phaseStart))
		return finishWithError(err)
	}
	updatePhase("waking", "done", "sent magic packet", time.Since(phaseStart))
	rerender(false)

	// Phase 3: Wait for initramfs SSH (poll locally, not via relay)
	updatePhase("initramfs", "start", "polling tcp/22", 0)
	rerender(false)
	phaseStart = time.Now()

	sshCtx, sshCanc := context.WithTimeout(context.Background(), 5*time.Minute)
	if err := pollSSHQuiet(sshCtx, target.IP+":22"); err != nil {
		sshCanc()
		updatePhase("initramfs", "error", err.Error(), time.Since(phaseStart))
		return finishWithError(err)
	}
	sshCanc()
	updatePhase("initramfs", "done", "initramfs ready", time.Since(phaseStart))
	rerender(false)

	// Phase 4: Interactive unlock
	// Clear the progress UI for interactive SSH
	updatePhase("unlock", "start", "running cryptroot-unlock", 0)
	rerender(false)
	phaseStart = time.Now()

	// Stop ticker to prevent overwriting the interactive SSH prompt
	close(stopTicker)

	// Clear progress display for interactive session
	clearLines(printedLines)
	fmt.Printf("%s Unlocking %s - enter LUKS passphrase:\n\n",
		colored(ansiYellow, "▶"),
		colored(ansiBold, target.Name))

	cmd := exec.Command("ssh", "-t", "root@"+target.IP, target.UnlockCommand)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Re-render progress after interactive session
		fmt.Println()
		printedLines = 0
		updatePhase("unlock", "error", err.Error(), time.Since(phaseStart))
		rerender(false)
		// Restart ticker so finishWithError can close it
		stopTicker = startTicker()
		return finishWithError(fmt.Errorf("cryptroot-unlock failed: %w", err))
	}

	// Re-render progress after interactive session
	fmt.Println()
	printedLines = 0
	updatePhase("unlock", "done", "disk unlocked", time.Since(phaseStart))
	rerender(false)

	// Restart ticker for remaining phases
	stopTicker = startTicker()

	// Phase 5: Wait for full system SSH on Tailscale hostname
	updatePhase("system", "start", fmt.Sprintf("polling %s:22", target.Name), 0)
	rerender(false)
	phaseStart = time.Now()

	sshCtx, sshCanc = context.WithTimeout(context.Background(), 5*time.Minute)
	if err := pollSSHQuiet(sshCtx, target.Name+":22"); err != nil {
		sshCanc()
		updatePhase("system", "error", err.Error(), time.Since(phaseStart))
		return finishWithError(err)
	}
	sshCanc()
	updatePhase("system", "done", "system ready", time.Since(phaseStart))

	return finish()
}
