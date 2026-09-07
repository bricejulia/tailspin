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
	readQuotaFlag := flag.Int("read-quota", 0, "Cloud Logging read requests per minute to stay under (default: 60, Cloud Logging's own default quota; also settable via $TAILSPIN_READ_QUOTA)")
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
	readQuota, err := config.ResolveReadQuota(*readQuotaFlag)
	if err != nil {
		return err
	}

	ctx := context.Background()
	client, err := gcplog.NewClient(ctx, project, readQuota)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	model := tui.New(client, project)
	model.SetReadQuota(readQuota)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("running tailspin: %w", err)
	}
	return nil
}
