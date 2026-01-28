package wakecli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var unlockCmd = &cobra.Command{
	Use:   "unlock <hostname>",
	Short: "Unlock LUKS encryption via SSH to initramfs",
	Long:  `Connect to a machine's initramfs via SSH for interactive LUKS passphrase entry.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host, err := lookupHost(args[0])
		if err != nil {
			return err
		}

		sshCmd := exec.CommandContext(cmd.Context(), "ssh",
			fmt.Sprintf("root@%s", host.IP),
		)
		sshCmd.Stdin = os.Stdin
		sshCmd.Stdout = os.Stdout
		sshCmd.Stderr = os.Stderr

		return sshCmd.Run()
	},
}
