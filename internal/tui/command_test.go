package tui

import "testing"

func TestParseCommand(t *testing.T) {
	cases := []struct {
		input   string
		want    parsedCommand
		wantErr bool
	}{
		{input: "tail", want: parsedCommand{Kind: cmdTail}},
		{input: "t", want: parsedCommand{Kind: cmdTail}},
		{input: "browse", want: parsedCommand{Kind: cmdBrowse}},
		{input: "b", want: parsedCommand{Kind: cmdBrowse}},
		{input: "project my-proj-123", want: parsedCommand{Kind: cmdProject, Arg: "my-proj-123"}},
		{input: "proj my-proj-123", want: parsedCommand{Kind: cmdProject, Arg: "my-proj-123"}},
		{input: "PROJECT my-proj-123", want: parsedCommand{Kind: cmdProject, Arg: "my-proj-123"}},
		{input: "help", want: parsedCommand{Kind: cmdHelp}},
		{input: "?", want: parsedCommand{Kind: cmdHelp}},
		{input: "quit", want: parsedCommand{Kind: cmdQuit}},
		{input: "q", want: parsedCommand{Kind: cmdQuit}},
		{input: "  tail  ", want: parsedCommand{Kind: cmdTail}},
		{input: "", wantErr: true},
		{input: "   ", wantErr: true},
		{input: "project", wantErr: true},
		{input: "bogus", wantErr: true},
	}

	for _, tc := range cases {
		got, err := parseCommand(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseCommand(%q) = %+v, want error", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseCommand(%q) returned unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseCommand(%q) = %+v, want %+v", tc.input, got, tc.want)
		}
	}
}
