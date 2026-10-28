package daemon

import (
	"net"
	"testing"
	"time"
)

// A client that goes away cancels its call; one still reading does not.
// Ctrl-C on `mobium wait` left the daemon waiting out the call, answering
// nothing else until it ended (CHALLENGES 375).
func TestWatchClientCancelsWhenTheClientGoes(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	ctx, cancel := watchClient(server)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("canceled while the client was still there")
	case <-time.After(200 * time.Millisecond):
	}

	client.Close()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("not canceled after the client went away")
	}
}
