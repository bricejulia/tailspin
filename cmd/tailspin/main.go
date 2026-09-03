// Command tailspin is a k9s-inspired terminal UI for exploring GCP Cloud
// Logging.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/bricejulia/tailspin/internal/config"
	"github.com/bricejulia/tailspin/internal/gcplog"
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

	// M1 milestone: prove the GCP wiring end-to-end with a plain-text
	// dump of the first page. The Bubble Tea TUI replaces this in M2+.
	page, err := client.ListEntries(ctx, gcplog.FilterState{}, "", 25)
	if err != nil {
		return err
	}

	fmt.Printf("tailspin %s — project %s — %d entries\n\n", version, project, len(page.Entries))
	for _, e := range page.Entries {
		fmt.Printf("%s  %-8s  %-30s  %s\n",
			e.Timestamp.Local().Format("2006-01-02 15:04:05"),
			e.Severity,
			truncate(e.LogName, 30),
			e.Summary,
		)
	}
	if page.NextPageToken != "" {
		fmt.Println("\n(more entries available — pagination lands in M2)")
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
