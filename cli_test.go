package main

import "testing"

func TestIsCLIInfoRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "help short", args: []string{"-h"}, want: true},
		{name: "help long", args: []string{"--help"}, want: true},
		{name: "version short", args: []string{"-v"}, want: true},
		{name: "version long", args: []string{"--version"}, want: true},
		{name: "file only", args: []string{"document.mdz"}, want: false},
		{name: "audience only", args: []string{"--audience", "token"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isCLIInfoRequest(test.args); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
