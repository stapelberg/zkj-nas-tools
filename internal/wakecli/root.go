package wakecli

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stapelberg/zkj-nas-tools/internal/wake"
)

var rootCmd = &cobra.Command{
	Use:   "wake",
	Short: "Control machine power states",
	Long:  `wake is a CLI for waking, suspending, and unlocking machines.`,
}

func init() {
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(suspendCmd)
	rootCmd.AddCommand(unlockCmd)
	rootCmd.AddCommand(resetCmd)
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func lookupHost(hostname string) (wake.Host, error) {
	host, ok := wake.Hosts[hostname]
	if !ok {
		validHostnames := strings.Join(slices.Sorted(maps.Keys(wake.Hosts)), ", ")
		return wake.Host{}, fmt.Errorf("unknown host %q, valid hosts: %s", hostname, validHostnames)
	}
	return host, nil
}
