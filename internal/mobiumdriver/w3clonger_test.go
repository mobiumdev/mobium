package mobiumdriver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A call the server holds open for longer than the client's timeout fails
// unless it is given the difference: an iOS background of 62 seconds timed
// out at sixty (CHALLENGES 157). The first request is the control that shows
// the timeout bites at all.
func TestALongerCallOutlastsTheClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"value":null}`))
	}))
	defer srv.Close()
	c := newW3CClient(100 * time.Millisecond)
	c.setBase(srv.URL)

	if err := c.do(context.Background(), http.MethodPost, "/held", nil, nil); err == nil {
		t.Fatal("a call held past the client's timeout succeeded, so this test cannot tell the fix from its absence")
	}
	if err := c.do(withLongerCall(context.Background(), time.Second), http.MethodPost, "/held", nil, nil); err != nil {
		t.Fatalf("given a second more, the held call still failed: %v", err)
	}
}
