package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/webconsole"
	"github.com/spf13/cobra"
)

func newWebCommand(options *rootOptions) *cobra.Command {
	var project string
	var port int
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the local Web console",
		Long:  "Start an authenticated Web console bound only to IPv4 loopback. The entry URL contains a process-session token in its URL fragment.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := config.Load(options.configPath)
			if err != nil {
				return fmt.Errorf("load Agent Manager configuration: %w", err)
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}
			console, err := webconsole.New(webconsole.Options{
				LibraryPath: settings.LibraryPath,
				HomeDir:     home,
				DataRoot:    filepath.Join(home, ".agents"),
				ProjectPath: project,
			})
			if err != nil {
				return err
			}
			listener, err := console.Listen(port)
			if err != nil {
				return err
			}
			defer listener.Close()

			entryURL := console.EntryURL()
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Agent Manager Web console listening on %s\nOpen %s\nPress Ctrl+C to stop.\n", listener.Addr(), entryURL); err != nil {
				return err
			}

			server := &http.Server{
				Handler:           console,
				ReadHeaderTimeout: 5 * time.Second,
				IdleTimeout:       60 * time.Second,
			}
			serveResult := make(chan error, 1)
			go func() { serveResult <- server.Serve(listener) }()
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			select {
			case err := <-serveResult:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return fmt.Errorf("serve local Web console: %w", err)
			case <-ctx.Done():
				shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := server.Shutdown(shutdownContext); err != nil {
					return fmt.Errorf("shut down local Web console: %w", err)
				}
				if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
					return fmt.Errorf("serve local Web console: %w", err)
				}
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "register this local project when the console starts")
	cmd.Flags().IntVar(&port, "port", 0, "loopback TCP port (0 selects an available port)")
	return cmd
}
