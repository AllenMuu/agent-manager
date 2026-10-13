package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AllenMuu/skill-manager/internal/config"
	"github.com/AllenMuu/skill-manager/internal/webconsole"
	"github.com/AllenMuu/skill-manager/internal/weblinks"
	"github.com/AllenMuu/skill-manager/internal/webui"
	"github.com/spf13/cobra"
)

func newWebCommand(options *rootOptions) *cobra.Command {
	var project string
	var port int
	var withInstallUI bool
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the local Web console",
		Long:  "Start an authenticated Web console bound only to IPv4 loopback. The entry URL contains a process-session token in its URL fragment. Add --with-install-ui and --project to start both local Web pages with mutual navigation links.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := config.Load(options.configPath)
			if err != nil {
				return fmt.Errorf("load Agent Manager configuration: %w", err)
			}
			if withInstallUI && project == "" {
				return fmt.Errorf("--project is required with --with-install-ui")
			}

			links := weblinks.New()
			var installServer *http.Server
			var installListener net.Listener
			var installURL string
			if withInstallUI {
				installURL, installServer, installListener, err = webui.NewLocalServerWithLinks(project, settings.LibraryPath, links)
				if err != nil {
					return err
				}
				defer installListener.Close()
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
				WebLinks:    links,
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
			links.SetConsoleURL(entryURL)
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Agent Manager Web console listening on %s\nOpen %s\n", listener.Addr(), entryURL); err != nil {
				return err
			}
			if withInstallUI {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Skill installation UI listening at %s\nOpen %s\n", installListener.Addr(), installURL); err != nil {
					return err
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "The two pages link to each other in their headers."); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl+C to stop."); err != nil {
				return err
			}

			consoleServer := &http.Server{
				Handler:           console,
				ReadHeaderTimeout: 5 * time.Second,
				IdleTimeout:       60 * time.Second,
			}
			servers := []*http.Server{consoleServer}
			listeners := []net.Listener{listener}
			if installServer != nil {
				servers = append(servers, installServer)
				listeners = append(listeners, installListener)
			}
			serveResult := make(chan error, len(servers))
			for index := range servers {
				server, serverListener := servers[index], listeners[index]
				go func() { serveResult <- server.Serve(serverListener) }()
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			select {
			case serveErr := <-serveResult:
				shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				for _, server := range servers {
					_ = server.Shutdown(shutdownContext)
				}
				if errors.Is(serveErr, http.ErrServerClosed) {
					return nil
				}
				return fmt.Errorf("serve local Web interface: %w", serveErr)
			case <-ctx.Done():
				shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				for _, server := range servers {
					if err := server.Shutdown(shutdownContext); err != nil {
						return fmt.Errorf("shut down local Web interface: %w", err)
					}
				}
				for range servers {
					if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
						return fmt.Errorf("serve local Web interface: %w", err)
					}
				}
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "register this local project when the console starts")
	cmd.Flags().IntVar(&port, "port", 0, "loopback TCP port (0 selects an available port)")
	cmd.Flags().BoolVar(&withInstallUI, "with-install-ui", false, "start the Skill installation UI and link the two Web pages (requires --project)")
	return cmd
}
