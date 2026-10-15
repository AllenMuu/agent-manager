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
		Short: "Start the local Web application",
		Long:  "Start the authenticated local Web application on IPv4 loopback. It opens on the management dashboard and provides one unified Skills workspace for recommendations, catalog browsing, details, and guarded activation or batch installation. The entry URL contains a process-session token in its URL fragment.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" {
				return fmt.Errorf("--project is required")
			}
			settings, err := config.Load(options.configPath)
			if err != nil {
				return fmt.Errorf("load Agent Manager configuration: %w", err)
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}
			console, err := webconsole.New(webconsole.Options{
				LibraryPath:       settings.LibraryPath,
				ConfigPath:        options.configPath,
				SyncMode:          string(settings.SyncMode),
				HomeDir:           home,
				DataRoot:          filepath.Join(home, ".agents"),
				ProjectPath:       project,
				SkillInstallation: true,
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
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Open Agent Manager Web at %s\n", entryURL); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl+C to stop."); err != nil {
				return err
			}

			consoleServer := &http.Server{
				Handler:           console,
				ReadHeaderTimeout: 5 * time.Second,
				IdleTimeout:       60 * time.Second,
			}
			serveResult := make(chan error, 1)
			go func() { serveResult <- consoleServer.Serve(listener) }()
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			select {
			case serveErr := <-serveResult:
				shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = consoleServer.Shutdown(shutdownContext)
				if errors.Is(serveErr, http.ErrServerClosed) {
					return nil
				}
				return fmt.Errorf("serve local Web interface: %w", serveErr)
			case <-ctx.Done():
				shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := consoleServer.Shutdown(shutdownContext); err != nil {
					return fmt.Errorf("shut down local Web interface: %w", err)
				}
				if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
					return fmt.Errorf("serve local Web interface: %w", err)
				}
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project served by the local Web application (required)")
	cmd.Flags().IntVar(&port, "port", 0, "loopback TCP port (0 selects an available port)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
