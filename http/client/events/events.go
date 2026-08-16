package events

import "net/http"

// RequestSending is dispatched before an HTTP request is sent.
type RequestSending struct {
	Request *http.Request
}

// ResponseReceived is dispatched after an HTTP response is received.
type ResponseReceived struct {
	Request  *http.Request
	Response *http.Response
}

// ConnectionFailed is dispatched when an HTTP request fails to connect.
type ConnectionFailed struct {
	Request   *http.Request
	Exception error
}
