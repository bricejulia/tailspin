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
