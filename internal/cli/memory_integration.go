package cli

import (
	"encoding/json"
	"fmt"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/memoryintegration"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
	"sync"
)

func newMemoryServeCommand() *cobra.Command {
	var access, events string
	cmd := &cobra.Command{Use: "serve-mcp", Short: "Expose only explicitly authorized shared project Memory over bounded stdio MCP", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := memoryintegration.LoadAccess(access)
		if err != nil {
			return memory.SafeError(err)
		}
		if events != "" {
			file, openErr := os.OpenFile(events, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if openErr != nil {
				return memory.ErrInvalidInput
			}
			defer file.Close()
			var eventMu sync.Mutex
			remaining := 1024 * 1024
			c.Observer = func(event memoryintegration.Event) {
				b, marshalErr := json.Marshal(event)
				if marshalErr != nil {
					return
				}
				eventMu.Lock()
				defer eventMu.Unlock()
				if len(b)+1 > remaining {
					return
				}
				remaining -= len(b) + 1
				_, _ = file.Write(append(b, '\n'))
			}
		}
		g, err := c.Gateway()
		if err != nil {
			return memory.SafeError(err)
		}
		reader, ok := cmd.InOrStdin().(io.ReadCloser)
		if !ok {
			reader = io.NopCloser(cmd.InOrStdin())
		}
		return memoryintegration.Serve(cmd.Context(), g, c.SessionConfig, reader, cmd.OutOrStdout())
	}}
	cmd.Flags().StringVar(&access, "access", "", "trusted session access configuration file")
	cmd.Flags().StringVar(&events, "events", "", "new task-owned metadata-only event file (bounded, no content mirror)")
	_ = cmd.MarkFlagRequired("access")
	return cmd
}
func newMemoryIntegrationConfigCommand() *cobra.Command {
	var agent, access, binary string
	cmd := &cobra.Command{Use: "integration-config", Short: "Print access-only native MCP configuration; no private memories or instructions are written", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if agent != "codex" && agent != "claude-code" || !filepath.IsAbs(access) || !filepath.IsAbs(binary) {
			return memory.ErrInvalidInput
		}
		if agent == "claude-code" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"mcpServers": map[string]any{"shared_memory": map[string]any{"command": binary, "args": []string{"memory", "serve-mcp", "--access", access}}}})
		}
		// JSON string quoting is also valid TOML basic string syntax for these paths.
		b, _ := json.Marshal(binary)
		a, _ := json.Marshal(access)
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "[mcp_servers.shared_memory]\ncommand = %s\nargs = [\"memory\", \"serve-mcp\", \"--access\", %s]\n", b, a)
		return err
	}}
	cmd.Flags().StringVar(&agent, "agent", "", "codex or claude-code")
	cmd.Flags().StringVar(&access, "access", "", "absolute trusted session access file")
	cmd.Flags().StringVar(&binary, "binary", "", "absolute Agent Manager executable")
	for _, flag := range []string{"agent", "access", "binary"} {
		_ = cmd.MarkFlagRequired(flag)
	}
	return cmd
}

func newMemoryIntegrationStatusCommand() *cobra.Command {
	var access string
	cmd := &cobra.Command{Use: "integration-status", Short: "Describe configured fixture-tested MCP operations; no native proof inferred from configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := memoryintegration.LoadAccess(access)
		if err != nil {
			return memory.SafeError(err)
		}
		g, err := c.Gateway()
		if err != nil {
			return memory.SafeError(err)
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(memoryintegration.ConfiguredStatus(g, c.SessionConfig))
	}}
	cmd.Flags().StringVar(&access, "access", "", "trusted session access file")
	_ = cmd.MarkFlagRequired("access")
	return cmd
}
