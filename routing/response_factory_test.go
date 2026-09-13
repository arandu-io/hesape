package routing_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpx "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/routing/exceptions"
)

func TestResponseFactoryBuildsTheExistingBufferedResponseTypes(t *testing.T) {
	factory := routing.NewResponseFactory(nil, nil)

	plain, err := factory.Make("created", http.StatusCreated, http.Header{"X-Test": []string{"plain"}})
	if err != nil {
		t.Fatalf("Make returned %v", err)
	}
	plainRecorder := httptest.NewRecorder()
	if err := plain.Send(plainRecorder); err != nil {
		t.Fatalf("plain Send returned %v", err)
	}
	if plainRecorder.Code != http.StatusCreated || plainRecorder.Body.String() != "created" || plainRecorder.Header().Get("X-Test") != "plain" {
		t.Fatalf("plain response = %d %q %q", plainRecorder.Code, plainRecorder.Body.String(), plainRecorder.Header().Get("X-Test"))
	}

	noContent, err := factory.NoContent(0, nil)
	if err != nil {
		t.Fatalf("NoContent returned %v", err)
	}
	noContentRecorder := httptest.NewRecorder()
	if err := noContent.Send(noContentRecorder); err != nil {
		t.Fatalf("no-content Send returned %v", err)
	}
	if noContentRecorder.Code != http.StatusNoContent || noContentRecorder.Body.Len() != 0 {
		t.Fatalf("no-content response = %d %q, want 204 empty", noContentRecorder.Code, noContentRecorder.Body.String())
	}

	jsonResponse, err := factory.JSON(map[string]any{"id": 42}, http.StatusAccepted, nil, 0)
	if err != nil {
		t.Fatalf("JSON returned %v", err)
	}
	jsonRecorder := httptest.NewRecorder()
	if err := jsonResponse.Send(jsonRecorder); err != nil {
		t.Fatalf("JSON Send returned %v", err)
	}
	if jsonRecorder.Code != http.StatusAccepted || jsonRecorder.Header().Get("Content-Type") != "application/json" || jsonRecorder.Body.String() != `{"id":42}` {
		t.Fatalf("JSON response = %d %q %q", jsonRecorder.Code, jsonRecorder.Header().Get("Content-Type"), jsonRecorder.Body.String())
	}

	jsonp, err := factory.JSONP("receive", map[string]any{"ok": true}, 0, nil, 0)
	if err != nil {
		t.Fatalf("JSONP returned %v", err)
	}
	jsonpRecorder := httptest.NewRecorder()
	if err := jsonp.Send(jsonpRecorder); err != nil {
		t.Fatalf("JSONP Send returned %v", err)
	}
	if jsonpRecorder.Header().Get("Content-Type") != "text/javascript" || jsonpRecorder.Body.String() != `/**/receive({"ok":true});` {
		t.Fatalf("JSONP response = %q %q", jsonpRecorder.Header().Get("Content-Type"), jsonpRecorder.Body.String())
	}
}

func TestResponseFactoryBuildsTheExistingStreamedResponseTypes(t *testing.T) {
	factory := routing.NewResponseFactory(nil, nil)

	stream := factory.Stream(func(ctx context.Context, writer io.Writer) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err := writer.Write([]byte("streamed"))
		return err
	}, http.StatusAccepted, http.Header{"X-Test": []string{"stream"}})
	recorder := httptest.NewRecorder()
	if err := stream.SendContent(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil)); err != nil {
		t.Fatalf("stream SendContent returned %v", err)
	}
	if recorder.Code != http.StatusAccepted || recorder.Body.String() != "streamed" || recorder.Header().Get("X-Test") != "stream" {
		t.Fatalf("stream response = %d %q %q", recorder.Code, recorder.Body.String(), recorder.Header().Get("X-Test"))
	}

	streamJSON := factory.StreamJSON(map[string]any{"id": 42}, 0, nil, httpx.JSONPrettyPrint)
	recorder = httptest.NewRecorder()
	if err := streamJSON.SendContent(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil)); err != nil {
		t.Fatalf("stream JSON SendContent returned %v", err)
	}
	if recorder.Header().Get("Content-Type") != "application/json" || !strings.Contains(recorder.Body.String(), `"id": 42`) {
		t.Fatalf("stream JSON response = %q %q", recorder.Header().Get("Content-Type"), recorder.Body.String())
	}

	wantErr := errors.New("read failed")
	download := factory.StreamDownload(func(context.Context, io.Writer) error {
		return wantErr
	}, "report.csv", nil, "")
	err := download.SendContent(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/download", nil))
	var streamedErr *exceptions.StreamedResponseError
	if !errors.As(err, &streamedErr) || !errors.Is(err, wantErr) {
		t.Fatalf("stream download error = %v, want wrapped read failure", err)
	}
}

func TestResponseFactoryBuildsFileAndRedirectResponses(t *testing.T) {
	factory := routing.NewResponseFactory(nil, nil)
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("report"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	fileRecorder := httptest.NewRecorder()
	if err := factory.File(path, http.Header{"X-Test": []string{"file"}}).SendContent(fileRecorder, httptest.NewRequest(http.MethodGet, "/report", nil)); err != nil {
		t.Fatalf("file SendContent returned %v", err)
	}
	if fileRecorder.Body.String() != "report" || fileRecorder.Header().Get("Content-Disposition") != "" || fileRecorder.Header().Get("X-Test") != "file" {
		t.Fatalf("file response = %q disposition %q X-Test %q", fileRecorder.Body.String(), fileRecorder.Header().Get("Content-Disposition"), fileRecorder.Header().Get("X-Test"))
	}

	downloadRecorder := httptest.NewRecorder()
	if err := factory.Download(path, "saved report.txt", nil, "").SendContent(downloadRecorder, httptest.NewRequest(http.MethodGet, "/report", nil)); err != nil {
		t.Fatalf("download SendContent returned %v", err)
	}
	if downloadRecorder.Body.String() != "report" || !strings.Contains(downloadRecorder.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("download response = %q disposition %q", downloadRecorder.Body.String(), downloadRecorder.Header().Get("Content-Disposition"))
	}

	request := httptest.NewRequest(http.MethodGet, "https://example.test/current", nil)
	generator := routing.NewUrlGenerator(routing.NewRoutes(), request)
	redirectFactory := routing.NewResponseFactory(nil, routing.NewRedirector(generator))
	redirect, err := redirectFactory.RedirectTo("/next", 0, http.Header{"X-Test": []string{"redirect"}}, nil)
	if err != nil {
		t.Fatalf("RedirectTo returned %v", err)
	}
	if redirect.Path != "https://example.test/next" || redirect.Status != http.StatusFound || redirect.Headers.Get("X-Test") != "redirect" {
		t.Fatalf("redirect = %#v", redirect)
	}
}
