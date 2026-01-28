package wakecli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var suspendCmd = &cobra.Command{
	Use:   "suspend <hostname>",
	Short: "Suspend a machine via SSH",
	Long:  `Suspend a machine by connecting via SSH with the ~/.ssh/id_suspend key.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host, err := lookupHost(args[0])
		if err != nil {
			return err
		}

		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}

		identityFile := filepath.Join(homeDir, ".ssh", "id_suspend")
		if _, err := os.Stat(identityFile); os.IsNotExist(err) {
			return fmt.Errorf("identity file not found: %s", identityFile)
		}

		sshCmd := exec.CommandContext(cmd.Context(), "ssh",
			"-i", identityFile,
			fmt.Sprintf("root@%s", host.IP),
		)
		sshCmd.Stdin = os.Stdin
		sshCmd.Stdout = os.Stdout
		sshCmd.Stderr = os.Stderr

		return sshCmd.Run()
	},
}
