package broadcasting_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// recordingBroadcaster refuses every channel and remembers the name it was
// asked about, so a test can see what the controller read from the body.
type recordingBroadcaster struct{ channel string }

func (b *recordingBroadcaster) Auth(_ context.Context, channel string) (auth.Grant, any, error) {
	b.channel = channel
	return auth.Grant{}, nil, auth.ErrForbidden
}

func (b *recordingBroadcaster) ValidAuthenticationResponse(context.Context, auth.Grant, broadcasting.Channel, any) (any, error) {
	return nil, nil
}

func (b *recordingBroadcaster) Broadcast(context.Context, auth.Grant, []broadcasting.Channel, string, map[string]any) error {
	return nil
}

type copiedKey struct{}

// TestAMultipartSubscriptionLeavesNoTemporaryFile: the controller read the
// channel with FormValue, which parses a multipart body outside the path that
// removes its temporary files. Behind any middleware that copies the request,
// a subscription posted with a large part left that part on disk for good.
func TestAMultipartSubscriptionLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	driver := &recordingBroadcaster{}
	manager := broadcasting.NewBroadcastManager(broadcasting.Config{
		Default:     "recording",
		Connections: map[string]broadcasting.ConnectionConfig{"recording": {Driver: "recording"}},
	}, nil, nil, nil)
	manager.Extend("recording", func(broadcasting.ConnectionConfig) (broadcasting.Broadcaster, error) {
		return driver, nil
	})
	controller := broadcasting.NewBroadcastController(manager)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controller.Authenticate(w, r.WithContext(context.WithValue(r.Context(), copiedKey{}, true)))
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.Start()
	defer srv.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField(broadcasting.ChannelNameField, "private-orders.17")
	part, err := writer.CreateFormFile("padding", "a.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(bytes.Repeat([]byte{0}, 40<<20))
	_ = writer.Close()

	resp, err := http.Post(srv.URL+"?"+broadcasting.ChannelNameField+"=presence-other", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if driver.channel != "private-orders.17" {
		t.Errorf("channel = %q, want the one the body sent and never the query string's", driver.channel)
	}

	var left []string
	for deadline := time.Now().Add(3 * time.Second); ; {
		left = left[:0]
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "multipart-") {
				left = append(left, entry.Name())
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(left) > 0 {
		t.Fatalf("%d multipart temporary file(s) outlived the request: %v", len(left), left)
	}
}
