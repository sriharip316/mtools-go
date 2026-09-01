package ui

import "testing"

func TestResolveEnabled(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		noColor    bool
		noColorEnv bool
		want       bool
		wantErr    bool
	}{
		{name: "always", mode: "always", want: true},
		{name: "always uppercase", mode: "ALWAYS", want: true},
		{name: "always bool spelling", mode: "true", want: true},
		{name: "never", mode: "never", want: false},
		{name: "never bool spelling", mode: "0", want: false},
		{name: "auto in test env", mode: "auto", want: false}, // stdout is not a TTY under go test
		{name: "empty mode is invalid", mode: "", wantErr: true},
		{name: "no-color wins over always", mode: "always", noColor: true, want: false},
		{name: "NO_COLOR env wins over always", mode: "always", noColorEnv: true, want: false},
		{name: "invalid mode", mode: "sometimes", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.noColorEnv {
				t.Setenv("NO_COLOR", "1")
			} else {
				t.Setenv("NO_COLOR", "")
			}

			got, err := ResolveEnabled(tc.mode, tc.noColor)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveEnabled(%q) err = nil, want error", tc.mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveEnabled(%q) unexpected error: %v", tc.mode, err)
			}
			if got != tc.want {
				t.Errorf("ResolveEnabled(%q, %v) = %v, want %v", tc.mode, tc.noColor, got, tc.want)
			}
		})
	}
}
