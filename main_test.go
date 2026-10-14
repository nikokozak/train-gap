package main

import (
	"testing"

	"github.com/nikokozak/train-gap/rides"
)

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

// chooseRide picks by name, or at random on a line, and says when a line has no rides.
func TestChooseRide(t *testing.T) {
	options := func(lineID, rideName string) rideOptions {
		return rideOptions{lineID: &lineID, rideName: &rideName}
	}
	shipped := rides.All()[0]

	if ride, err := options("", shipped.Name).chooseRide(); err != nil || ride.Name != shipped.Name {
		t.Fatalf("by name: %s, %v; want %s", ride.Name, err, shipped.Name)
	}
	if ride, err := options(shipped.LineID, "").chooseRide(); err != nil || ride.LineID != shipped.LineID {
		t.Fatalf("on the %s: %+v, %v", shipped.LineID, ride, err)
	}
	if _, err := options("", "no-such-ride").chooseRide(); err == nil {
		t.Fatal("an unknown ride name gave no error")
	}
	if _, err := options("no-such-line", "").chooseRide(); err == nil {
		t.Fatal("a line without rides gave no error")
	}
}
