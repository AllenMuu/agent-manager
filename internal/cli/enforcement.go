package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/spf13/cobra"
)

func newEnforcementCommand(name string, jsonOutput *bool) *cobra.Command {
	return &cobra.Command{Use: name + " <request.json>", Short: "Inspect offline permission declarations; never launch or attest a runtime", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		file, err := os.Open(args[0])
		if err != nil {
			return fmt.Errorf("open preflight request: %w", err)
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		if err != nil {
			return fmt.Errorf("read preflight request: %w", err)
		}
		if len(data) > 1<<20 {
			return fmt.Errorf("preflight request exceeds 1 MiB")
		}
		r, err := enforcement.LoadRequest(data)
		if err != nil {
			return err
		}
		result := enforcement.Preflight(r)
		if *jsonOutput {
			err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		} else {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Offline declarations only; execution is not authorized or observed.\nAccepted: %t\n", result.Accepted)
			if err == nil {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
		}
		if err != nil {
			return err
		}
		if name == "preflight" && !result.Accepted {
			return fmt.Errorf("offline preflight rejected: inspect errors for missing dimensions")
		}
		return nil
	}}
}
