# tailspin

A [k9s](https://k9scli.io/)-inspired terminal UI for exploring [GCP Cloud
Logging](https://cloud.google.com/logging), built with [Bubble
Tea](https://github.com/charmbracelet/bubbletea).

Browse, filter, and live-tail log entries for a project without leaving the
terminal — no `gcloud logging read` incantations, no Cloud Console tab.

## Why

tailspin talks to Cloud Logging directly through the Go client libraries
(not by shelling out to `gcloud`), so live tailing uses the real
`TailLogEntries` streaming RPC instead of parsing a CLI's output.

## Install

```sh
go install github.com/bricejulia/tailspin/cmd/tailspin@latest
```

Or build from a clone of this repo:

```sh
go build -o tailspin ./cmd/tailspin
```

## Setup

tailspin reuses your existing `gcloud` credentials — no separate login flow.

```sh
gcloud auth application-default login
```

That's it if you already use `gcloud`. tailspin picks a project the same
way `gcloud` does, in this order:

1. `--project <id>` flag
2. `$GOOGLE_CLOUD_PROJECT` or `$TAILSPIN_PROJECT` environment variable
3. gcloud's active configuration (`gcloud config set project <id>`)

## Usage

```sh
tailspin                        # uses the resolved default project
tailspin --project my-project   # or pin one explicitly
tailspin --version
```

## Keybindings

| Key | Action |
|---|---|
| `j` / `k`, `↓` / `↑` | Move selection |
| `pgup` / `pgdn` | Page up / down |
| `enter` | View full entry detail |
| `esc` | Back |
| `/` | Filter (severity, log name, free text, time range) |
| `:` | Command mode (see below) |
| `t` | Tail (live streaming) |
| `w` | Toggle wrap: one-line-per-entry + `h`/`l` horizontal scroll, or full reflow with hanging indent |
| `r` | Refresh (re-run the current query from page 1) |
| `?` | Help |
| `q` | Quit |

**In the filter bar (`/`):** `tab` / `shift+tab` cycles between severity,
log name, free text, and time-range fields; `←`/`→` (or `h`/`l`) cycles the
severity and time-range fields; `enter` runs the query; `esc` cancels.

**In tail mode (`t`):** `space` or `p` pauses/resumes autoscroll — new
entries keep arriving and are kept while paused, so nothing is lost while
you're reading an earlier point in the stream. `esc` stops the stream and
returns to browse.

## `:` commands

| Command | Aliases | Action |
|---|---|---|
| `:browse` | `:b` | Return to browse mode |
| `:tail` | `:t` | Switch to tail mode |
| `:project <id>` | `:p <id>` | Switch to a different GCP project |
| `:help` | `:h`, `:?` | Show the help screen |
| `:quit` | `:q` | Quit |

## Development

```sh
make build   # compile ./tailspin
make test    # go test -race ./...
make vet     # go vet ./...
make lint    # golangci-lint (see .golangci.yml)
```

`internal/gcplog` never gets mocked in tests — it's the one package that
talks to GCP, kept behind a small `Client` interface with a hand-written
fake so `internal/tui` can be tested without any network access. See
`internal/gcplog/filter_test.go` and `internal/config/config_test.go` for
the parts of the GCP wiring that are pure functions and directly testable.
