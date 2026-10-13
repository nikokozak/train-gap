package main

import "testing"

// run passes the command's exit code through and gives the command the proxy environment.
func TestRunCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  []string
		wantCode int
	}{
		{"exit code passes through", []string{"sh", "-c", "exit 3"}, 3},
		{"the proxy environment is set", []string{"sh", "-c", `case "$ALL_PROXY $https_proxy $NODE_USE_ENV_PROXY" in "http://127.0.0.1:"*" http://127.0.0.1:"*" 1") exit 0;; *) exit 1;; esac`}, 0},
		{"a missing command", []string{"/nonexistent/command"}, 127},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			arguments := append([]string{"--speed", "10", "--"}, test.command...)
			if got := runCommand(arguments); got != test.wantCode {
				t.Fatalf("runCommand(%v) = %d; want %d", arguments, got, test.wantCode)
			}
		})
	}
}
