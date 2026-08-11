package events

import "net/http"

// PreparingResponse is emitted before the router sends the response.
//
// It mirrors Illuminate\Routing\Events\PreparingResponse.
type PreparingResponse struct {
	Request  *http.Request
	Response http.ResponseWriter
}

// ResponsePrepared is emitted after the router has prepared the response
// but before it is sent.
//
// It mirrors Illuminate\Routing\Events\ResponsePrepared.
type ResponsePrepared struct {
	Request  *http.Request
	Response http.ResponseWriter
}

// RouteMatched is emitted when a route matches the incoming request.
//
// It mirrors Illuminate\Routing\Events\RouteMatched.
type RouteMatched struct {
	Route   RouteInfo
	Request *http.Request
}

// RouteInfo is the information available when a route matches.
type RouteInfo interface {
	GetName() string
	GetActionName() string
}

// Routing is emitted before the router begins dispatching the request.
//
// It mirrors Illuminate\Routing\Events\Routing.
type Routing struct {
	Request *http.Request
}
