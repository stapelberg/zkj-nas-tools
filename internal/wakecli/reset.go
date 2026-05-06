package wakecli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/stapelberg/zkj-nas-tools/internal/wake"
)

var resetCmd = &cobra.Command{
	Use:          "reset <hostname>",
	Short:        "Reset a machine via smart plug power cycle",
	Long:         `Reset a machine by first shutting it down via SSH with the ~/.ssh/id_poweroff key, then cutting smart plug relay power, waiting for power to drop, and restoring relay power so WOL works again. Use --force to skip the SSH shutdown.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, err := lookupHost(args[0])
		if err != nil {
			return err
		}

		if host.SmartPlug == "" {
			return fmt.Errorf("host %q has no smart plug configured", host.Name)
		}

		force, err := cmd.Flags().GetBool("force")
		if err != nil {
			return err
		}

		if !force {
			if err := sshShutdown(cmd.Context(), host); err != nil {
				return err
			}
			return nil
		}

		log.Printf("power-cycling %s via smart plug %s", host.Name, host.SmartPlug)
		if err := wake.PowerCycleSmartPlug(cmd.Context(), host.SmartPlug); err != nil {
			return err
		}
		log.Printf("reset of %s complete", host.Name)
		return nil
	},
}

func init() {
	resetCmd.Flags().Bool("force", false, "skip SSH shutdown, cut relay power immediately")
}

func sshShutdown(ctx context.Context, host wake.Host) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	identityFile := filepath.Join(homeDir, ".ssh", "id_poweroff")
	if _, err := os.Stat(identityFile); os.IsNotExist(err) {
		return fmt.Errorf("identity file not found: %s", identityFile)
	}

	log.Printf("sending shutdown command to %s via SSH", host.Name)
	sshCmd := exec.CommandContext(ctx, "ssh",
		"-i", identityFile,
		fmt.Sprintf("root@%s", host.IP),
	)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	if err := sshCmd.Run(); err != nil {
		// SSH errors during shutdown are expected (connection closed by remote)
		log.Printf("SSH exited with error (expected during shutdown): %v", err)
	}
	return nil
}
