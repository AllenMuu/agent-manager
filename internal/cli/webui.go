package cli

import (
	"fmt"

	"github.com/AllenMuu/skill-manager/internal/webui"
	"github.com/spf13/cobra"
)

func newWebUICommand(options *rootOptions) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "webui",
		Short: "Open the local project Skill installation interface",
		RunE: func(cmd *cobra.Command, _ []string) error {
			lib, err := library(options)
			if err != nil {
				return err
			}
			url, server, listener, err := webui.NewLocalServer(project, lib)
			if err != nil {
				return err
			}
			defer listener.Close()
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "WebUI: %s\nPress Ctrl+C to stop.\n", url); err != nil {
				return err
			}
			return server.Serve(listener)
		},
	}
	projectFlag(cmd, &project)
	return cmd
}
