package tui

import (
	"fmt"
	"strings"
)

// commandKind identifies a parsed ":" command.
type commandKind int

const (
	cmdUnknown commandKind = iota
	cmdTail
	cmdBrowse
	cmdProject
	cmdHelp
	cmdQuit
	cmdQuery
	cmdSave
	cmdLoad
	cmdQueries
)

// parsedCommand is the result of parsing a ":" command line.
type parsedCommand struct {
	Kind commandKind
	Arg  string // e.g. the project ID for cmdProject
}

// parseCommand parses the text typed after ":" (k9s-style command mode) into
// a parsedCommand. It's a pure function with no Bubble Tea or I/O
// dependency, so it's unit-testable on its own.
func parseCommand(input string) (parsedCommand, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return parsedCommand{}, fmt.Errorf("empty command")
	}

	name := strings.ToLower(fields[0])
	switch name {
	case "tail", "t":
		return parsedCommand{Kind: cmdTail}, nil
	case "browse", "b":
		return parsedCommand{Kind: cmdBrowse}, nil
	case "project", "proj", "p":
		if len(fields) < 2 {
			return parsedCommand{}, fmt.Errorf("usage: project <id>")
		}
		return parsedCommand{Kind: cmdProject, Arg: fields[1]}, nil
	case "help", "h", "?":
		return parsedCommand{Kind: cmdHelp}, nil
	case "quit", "q":
		return parsedCommand{Kind: cmdQuit}, nil
	case "query", "raw":
		return parsedCommand{Kind: cmdQuery}, nil
	case "save":
		if len(fields) < 2 {
			return parsedCommand{}, fmt.Errorf("usage: save <name>")
		}
		return parsedCommand{Kind: cmdSave, Arg: fields[1]}, nil
	case "load":
		if len(fields) < 2 {
			return parsedCommand{}, fmt.Errorf("usage: load <name>")
		}
		return parsedCommand{Kind: cmdLoad, Arg: fields[1]}, nil
	case "queries":
		return parsedCommand{Kind: cmdQueries}, nil
	default:
		return parsedCommand{}, fmt.Errorf("unknown command %q", name)
	}
}
