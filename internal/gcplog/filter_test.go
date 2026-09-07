package gcplog

import (
	"testing"
	"time"

	"cloud.google.com/go/logging"
)

func TestFilterStateBuild(t *testing.T) {
	fixedTime := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		f    FilterState
		want string
	}{
		{
			name: "empty",
			f:    FilterState{},
			want: "",
		},
		{
			name: "severity only",
			f:    FilterState{MinSeverity: logging.Warning},
			want: `severity>=WARNING`,
		},
		{
			name: "default severity omitted",
			f:    FilterState{MinSeverity: logging.Default},
			want: "",
		},
		{
			name: "log name only",
			f:    FilterState{LogName: "syslog"},
			want: `logName:"syslog"`,
		},
		{
			name: "exact severity only",
			f:    FilterState{ExactSeverity: logging.Warning},
			want: `severity=WARNING`,
		},
		{
			name: "exact severity wins over min severity",
			f:    FilterState{MinSeverity: logging.Error, ExactSeverity: logging.Warning},
			want: `severity=WARNING`,
		},
		{
			name: "single label",
			f:    FilterState{Labels: map[string]string{"env": "prod"}},
			want: `labels."env"="prod"`,
		},
		{
			name: "multiple labels sorted by key",
			f:    FilterState{Labels: map[string]string{"zone": "us-east1", "env": "prod"}},
			want: `labels."env"="prod" AND labels."zone"="us-east1"`,
		},
		{
			name: "label value needing escaping",
			f:    FilterState{Labels: map[string]string{"msg": `say "hi"`}},
			want: `labels."msg"="say \"hi\""`,
		},
		{
			name: "time range only",
			f:    FilterState{Since: fixedTime, Until: fixedTime.Add(time.Hour)},
			want: `timestamp>="2026-09-03T12:00:00Z" AND timestamp<="2026-09-03T13:00:00Z"`,
		},
		{
			name: "free text needing escaping",
			f:    FilterState{FreeText: `say "hi"`},
			want: `SEARCH("say \"hi\"")`,
		},
		{
			name: "all fields combined",
			f: FilterState{
				MinSeverity:  logging.Error,
				LogName:      "syslog",
				ResourceType: "gce_instance",
				FreeText:     "boom",
				Since:        fixedTime,
			},
			want: `severity>=ERROR AND logName:"syslog" AND resource.type="gce_instance" AND SEARCH("boom") AND timestamp>="2026-09-03T12:00:00Z"`,
		},
		{
			name: "all fields combined including exact severity and labels",
			f: FilterState{
				MinSeverity:   logging.Error,
				ExactSeverity: logging.Critical,
				LogName:       "syslog",
				ResourceType:  "gce_instance",
				FreeText:      "boom",
				Labels:        map[string]string{"env": "prod"},
				Since:         fixedTime,
			},
			want: `severity=CRITICAL AND logName:"syslog" AND resource.type="gce_instance" AND SEARCH("boom") AND labels."env"="prod" AND timestamp>="2026-09-03T12:00:00Z"`,
		},
		{
			name: "raw query alone",
			f:    FilterState{RawQuery: `resource.type="k8s_container"`},
			want: `(resource.type="k8s_container")`,
		},
		{
			name: "raw query with time bound",
			f: FilterState{
				RawQuery: `resource.type="k8s_container"`,
				Since:    fixedTime,
			},
			want: `(resource.type="k8s_container") AND timestamp>="2026-09-03T12:00:00Z"`,
		},
		{
			name: "raw query with embedded newline round-trips unchanged",
			f: FilterState{
				RawQuery: "resource.type=\"k8s_container\"\nresource.labels.cluster_name=\"my-cluster\"",
			},
			want: "(resource.type=\"k8s_container\"\nresource.labels.cluster_name=\"my-cluster\")",
		},
		{
			name: "raw query wins over structured fields",
			f: FilterState{
				MinSeverity: logging.Error,
				LogName:     "syslog",
				RawQuery:    `resource.type="k8s_container"`,
			},
			want: `(resource.type="k8s_container")`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.f.Build()
			if got != tc.want {
				t.Errorf("Build() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFilterStateQuery(t *testing.T) {
	cases := []struct {
		name string
		f    FilterState
		want string
	}{
		{
			name: "empty",
			f:    FilterState{},
			want: "",
		},
		{
			name: "structured fields, no time bound",
			f:    FilterState{MinSeverity: logging.Error, LogName: "syslog"},
			want: `severity>=ERROR AND logName:"syslog"`,
		},
		{
			name: "since is excluded even when set",
			f: FilterState{
				MinSeverity: logging.Warning,
				Since:       time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
			},
			want: `severity>=WARNING`,
		},
		{
			name: "raw query returned verbatim, untrimmed input trimmed",
			f:    FilterState{RawQuery: "  resource.type=\"k8s_container\"  "},
			want: `resource.type="k8s_container"`,
		},
		{
			name: "raw query wins over structured fields",
			f:    FilterState{MinSeverity: logging.Error, RawQuery: `resource.type="k8s_container"`},
			want: `resource.type="k8s_container"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.f.Query()
			if got != tc.want {
				t.Errorf("Query() = %q, want %q", got, tc.want)
			}
		})
	}
}
