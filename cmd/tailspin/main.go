// Command tailspin is a k9s-inspired terminal UI for exploring GCP Cloud Logging.
package main

import (
	"fmt"
	"os"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("tailspin " + version)
		return
	}

	fmt.Println("tailspin " + version + " — GCP log explorer (scaffolding only, no TUI yet)")
}
