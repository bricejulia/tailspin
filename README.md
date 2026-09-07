# tailspin


[![CI](https://github.com/bricejulia/tailspin/actions/workflows/ci.yml/badge.svg)](https://github.com/bricejulia/tailspin/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

![tailspin banner](https://github.com/bricejulia/tailspin/blob/main/assets/tailspin.png?raw=true)

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

```bash
curl -fsSL https://raw.githubusercontent.com/bricejulia/tailspin/main/install.sh | sh
```

Or with Homebrew:

```bash
brew install bricejulia/tap/tailspin
```

> **macOS:** if Gatekeeper blocks the binary on first run, remove the quarantine attribute:
> `xattr -d com.apple.quarantine $(which tailspin)`, or allow it manually in
> System Settings → Privacy & Security.

Or with Go:

```bash
go install github.com/bricejulia/tailspin/cmd/tailspin@latest
```

You can also download the latest binaries from the [release page](https://github.com/bricejulia/tailspin/releases). If you use this method, don't forget to check for updates regularly!


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

tailspin also paces its own Cloud Logging read requests (list pagination,
the histogram's per-bucket queries — see Keybindings below) to a
requests-per-minute budget, so normal use doesn't trip your project's read
quota. It defaults to 60, Cloud Logging's own default quota; override it
with `--read-quota <n>` or `$TAILSPIN_READ_QUOTA` if your project's quota is
different (raised, or shared with other tools already consuming part of
it) — a higher budget means a faster, more complete histogram; a lower one
means a slower, more often partial one (flagged with a "data incomplete" /
"~" marker rather than failing outright).

## Usage

```sh
tailspin                                    # uses the resolved default project
tailspin --project my-project               # or pin one explicitly
tailspin --read-quota 300                   # raise the read-requests-per-minute budget
tailspin --version
```

## Keybindings

A stacked, severity-colored histogram sits above the log view (browse and
tail modes, terminal permitting) — click a bar or drag across several to set
the time range to what you selected and re-run the query, GCP Log
Explorer-style.

| Key | Action |
|---|---|
| `j` / `k`, `↓` / `↑` | Move selection |
| `pgup` / `pgdn` | Scroll by a screenful |
| `n` / `N` | Next / previous page — free within already-loaded pages, fetches more once you're on the last one |
| `enter` | View full entry detail |
| `esc` | Back |
| `/` | Filter (severity, log name, free text, time range) |
| `:` | Command mode (see below) |
| `t` | Tail (live streaming) |
| `w` | Toggle wrap: by default every row is one line — `h`/`l` scrolls the table sideways to read the rest (`0`/`$` jump straight to the start/end); wrap instead reflows every row across as many lines as its message needs |
| `r` | Refresh (re-run the current query from page 1) |
| `?` | Help |
| `q` | Quit |

**In the filter bar (`/`):** `tab` / `shift+tab` cycles between severity,
log name, free text, and time-range fields; `←`/`→` (or `h`/`l`) cycles the
severity and time-range fields; `enter` runs the query; `esc` cancels —
from any field, text inputs included.

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
| `:query` | `:raw` | Open the raw/advanced filter editor — `ctrl+s` runs it, `esc` cancels |
| `:save <name>` | | Save the active query under `<name>` |
| `:load <name>` | | Load a saved query by name |
| `:queries` | | List saved queries (`j`/`k` select, `enter` runs one) |
| `:help` | `:h`, `:?` | Show the help screen |
| `:quit` | `:q` | Quit |

## Advanced queries

The filter bar (`/`) covers severity, log name, free text, and time range,
but can't express arbitrary comparisons like `resource.labels.*`. For that,
`:query` (or `:raw`) opens a full multi-line editor — paste or type any
Cloud Logging filter expression, including the kind Cloud Console's own
query builder produces:

```
resource.type="k8s_container"
resource.labels.cluster_name="my-cluster"
resource.labels.container_name="my-container"
resource.labels.namespace_name="my-namespace"
```

(Cloud Logging treats newline-separated clauses as implicitly ANDed, same
as explicit `AND`.) `ctrl+s` runs it — a time-range bound is still applied
on top automatically, same as browse mode's default. A raw query replaces
the filter bar's structured fields (not combined with them); submitting
the filter bar the normal way switches back.

## Saved queries

`:save <name>` saves the active query (the raw query if one's active,
otherwise whatever the filter bar currently builds) under `<name>`,
independent of any time range. `:load <name>` restores it. `:queries`
lists what's saved — `j`/`k` to select, `enter` to run one directly.

Saved queries live in a plain JSON file at
`$XDG_CONFIG_HOME/tailspin/queries.json` (falling back to
`~/.config/tailspin/queries.json`) that you can hand-edit or pre-populate
— tailspin only ever reads/writes `name` and `filter`; `description` is
never set by the app, purely for your own reference:

```json
[
  {
    "name": "k8s-my-container",
    "description": "optional, hand-edit only",
    "filter": "resource.type=\"k8s_container\"\nresource.labels.cluster_name=\"my-cluster\""
  }
]
```

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


## Status

tailspin is a work in progress and a learning project.