package cli_test

import (
	"testing"

	"github.com/AllenMuu/skill-manager/internal/cli"
)

func TestWebCommandExposesOnlyProjectAndLoopbackPortOptions(t *testing.T) {
	root := cli.NewAgentManagerCommand()
	command, _, err := root.Find([]string{"web"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Flags().Lookup("project") == nil || command.Flags().Lookup("port") == nil {
		t.Fatal("web command is missing project or port flag")
	}
	if got := command.Flags().Lookup("port").DefValue; got != "0" {
		t.Fatalf("default port = %q, want ephemeral port 0", got)
	}
	if command.Flags().Lookup("host") != nil || command.Flags().Lookup("bind") != nil {
		t.Fatal("web command must not expose a non-loopback bind-address flag")
	}
}
