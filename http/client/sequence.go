package client

import (
	"net/http"
	"os"
)

// ResponseSequence provides an ordered sequence of stub responses that are
// returned one at a time as requests are made. When the sequence is
// exhausted, the next request returns an error (or a default if one is
// configured).
type ResponseSequence struct {
	responses     []sequenceEntry
	index         int
	whenEmpty     sequenceEntry
	failWhenEmpty bool
}

type sequenceEntry struct {
	status  int
	body    []byte
	headers http.Header
	err     error
}

// NewResponseSequence creates an empty ResponseSequence.
func NewResponseSequence() *ResponseSequence {
	return &ResponseSequence{
		failWhenEmpty: true,
	}
}

// Push adds a response to the sequence.
func (s *ResponseSequence) Push(status int, body string, headers http.Header) *ResponseSequence {
	s.responses = append(s.responses, sequenceEntry{
		status:  status,
		body:    []byte(body),
		headers: headers,
	})
	return s
}

// PushStatus adds a response with only a status code (no body).
func (s *ResponseSequence) PushStatus(status int, headers http.Header) *ResponseSequence {
	return s.Push(status, "", headers)
}

// PushFile adds a response whose body is read from the given file path.
func (s *ResponseSequence) PushFile(filePath string, status int, headers http.Header) *ResponseSequence {
	body, err := os.ReadFile(filePath)
	if err != nil {
		s.responses = append(s.responses, sequenceEntry{err: err})
		return s
	}
	return s.Push(status, string(body), headers)
}

// PushFailedConnection adds a connection failure to the sequence.
func (s *ResponseSequence) PushFailedConnection(message string) *ResponseSequence {
	if message == "" {
		message = "connection refused"
	}
	s.responses = append(s.responses, sequenceEntry{
		err: NewConnectionException(message),
	})
	return s
}

// PushResponse adds a raw *http.Response to the sequence.
func (s *ResponseSequence) PushResponse(resp *http.Response) *ResponseSequence {
	s.responses = append(s.responses, sequenceEntry{status: resp.StatusCode, headers: resp.Header})
	return s
}

// WhenEmpty configures the default response returned when the sequence is
// exhausted. When set, the sequence does not fail when empty.
func (s *ResponseSequence) WhenEmpty(status int, body string, headers http.Header) *ResponseSequence {
	s.whenEmpty = sequenceEntry{status: status, body: []byte(body), headers: headers}
	s.failWhenEmpty = false
	return s
}

// DontFailWhenEmpty prevents the sequence from returning an error when
// exhausted.
func (s *ResponseSequence) DontFailWhenEmpty() *ResponseSequence {
	s.failWhenEmpty = false
	return s
}

// IsEmpty reports whether the sequence has any remaining responses.
func (s *ResponseSequence) IsEmpty() bool {
	return s.index >= len(s.responses)
}

// AsStub returns a StubCallback that consumes responses from this sequence.
func (s *ResponseSequence) AsStub(urlPattern string) StubCallback {
	return func(req *http.Request) (*http.Response, error) {
		if urlPattern != "*" && req.URL.String() != urlPattern && req.URL.Path != urlPattern {
			return nil, nil
		}

		if s.index >= len(s.responses) {
			if s.failWhenEmpty {
				return nil, NewConnectionException("response sequence is empty")
			}
			return newHTTPResponse(s.whenEmpty), nil
		}

		entry := s.responses[s.index]
		s.index++

		if entry.err != nil {
			return nil, entry.err
		}

		return newHTTPResponse(entry), nil
	}
}

func newHTTPResponse(e sequenceEntry) *http.Response {
	headers := e.headers
	if headers == nil {
		headers = http.Header{}
	}
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}
	status := e.status
	if status == 0 {
		status = 200
	}
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       ioReadCloser(e.body),
		Status:     http.StatusText(status),
	}
}

// ioReadCloser wraps a byte slice as io.ReadCloser.
type byteReadCloser struct {
	data   []byte
	offset int
	closed bool
}

func ioReadCloser(data []byte) *byteReadCloser {
	return &byteReadCloser{data: data}
}

func (b *byteReadCloser) Read(p []byte) (int, error) {
	if b.closed {
		return 0, os.ErrClosed
	}
	if b.offset >= len(b.data) {
		return 0, nil
	}
	n := copy(p, b.data[b.offset:])
	b.offset += n
	return n, nil
}

func (b *byteReadCloser) Close() error {
	b.closed = true
	return nil
}
