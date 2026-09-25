package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/spf13/cobra"
)

func newAgentsCommand() *cobra.Command {
	var asJSON bool
	var project string
	cmd := &cobra.Command{Use: "agents", Short: "List supported agent adapters", RunE: func(cmd *cobra.Command, _ []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		items, err := adapter.Inventory(home, project)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
		}
		for _, item := range items {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%v\t%v\n", item.ID, item.Status, item.Availability, item.ResourceKinds, item.Capabilities); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "write machine-readable JSON")
	cmd.Flags().StringVar(&project, "project", "", "project directory whose agent locations should be inventoried")
	return cmd
}
