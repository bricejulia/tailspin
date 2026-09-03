// Command tailspin is a k9s-inspired terminal UI for exploring GCP Cloud
// Logging.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/bricejulia/tailspin/internal/config"
	"github.com/bricejulia/tailspin/internal/gcplog"
	"github.com/bricejulia/tailspin/internal/tui"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tailspin: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	projectFlag := flag.String("project", "", "GCP project ID (defaults to $GOOGLE_CLOUD_PROJECT, $TAILSPIN_PROJECT, or gcloud's active config)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("tailspin " + version)
		return nil
	}

	project, err := config.ResolveProject(*projectFlag)
	if err != nil {
		return err
	}

	ctx := context.Background()
	client, err := gcplog.NewClient(ctx, project)
	if err != nil {
		return err
	}
	defer client.Close()

	model := tui.New(client, project)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("running tailspin: %w", err)
	}
	return nil
}
