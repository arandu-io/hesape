package rpc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/arandu-io/hesape/auth"
)

var errAuthenticationRequired = errors.New("authentication required")

// AuthenticateBearer resolves a bearer token into the subject that an
// application Service passes to its Policy. It must not authorize an action or
// construct a Grant.
type AuthenticateBearer func(context.Context, string) (auth.Subject, error)

// Config contains the mandatory transport limits for an RPC service.
type Config struct {
	ReadMaxBytes           int
	SendMaxBytes           int
	RequestMaxBytes        int64
	MaxConcurrent          int
	MaxConcurrentPerTenant int
	MaxDuration            time.Duration
	ServerTimeout          time.Duration
	AllowedOrigins         []string
	AllowedHeaders         []string
}

// HandlerFactory closes over an application Service and constructs its
// generated ConnectRPC handler with the supplied mandatory options.
type HandlerFactory func(...connect.HandlerOption) (string, http.Handler)

// Handler serves one generated service and can close admission before the
// application begins its normal HTTP shutdown.
type Handler struct {
	next      http.Handler
	admission *admission
}

// ServeHTTP implements [http.Handler].
func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	h.next.ServeHTTP(response, request)
}

// CloseAdmission refuses new RPC calls while calls already inside the
// application Service continue under their existing contexts and deadline.
func (h *Handler) CloseAdmission() {
	h.admission.closed.Store(true)
}

// NewHandler constructs a generated service through the mandatory transport
// boundary. The returned handler supports Connect, gRPC, and gRPC-Web and is
// intended to be mounted on the application's existing HTTP router.
func NewHandler(factory HandlerFactory, authenticate AuthenticateBearer, config Config) (string, *Handler, error) {
	if factory == nil {
		return "", nil, errors.New("rpc: handler factory is required")
	}
	if authenticate == nil {
		return "", nil, errors.New("rpc: bearer authenticator is required")
	}
	if err := config.validate(); err != nil {
		return "", nil, err
	}

	limit := newCallLimiter(config.MaxConcurrent, config.MaxConcurrentPerTenant)
	admission := &admission{}
	path, handler := factory(
		connect.WithRequestGate(admission.gate),
		connect.WithRequestGate(authenticationGate(authenticate)),
		connect.WithInterceptors(limit),
		connect.WithReadMaxBytes(config.ReadMaxBytes),
		connect.WithSendMaxBytes(config.SendMaxBytes),
		connect.WithRequireConnectProtocolHeader(),
		connect.WithRecover(func(context.Context, connect.Spec, http.Header, any) error {
			return connect.NewError(connect.CodeInternal, errors.New("internal error"))
		}),
	)
	if handler == nil {
		return "", nil, errors.New("rpc: handler factory returned a nil handler")
	}
	if path == "" || path[0] != '/' || path[len(path)-1] != '/' {
		return "", nil, errors.New("rpc: handler factory returned an invalid path")
	}

	handler = corsHandler(path, handler, config.AllowedOrigins, config.AllowedHeaders)
	handler = http.MaxBytesHandler(handler, config.RequestMaxBytes)
	limitedHandler := handler
	handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), config.MaxDuration)
		defer cancel()
		limitedHandler.ServeHTTP(response, request.WithContext(ctx))
	})
	return path, &Handler{next: handler, admission: admission}, nil
}

func (c Config) validate() error {
	if c.ReadMaxBytes <= 0 {
		return errors.New("rpc: read max bytes must be positive")
	}
	if c.SendMaxBytes <= 0 {
		return errors.New("rpc: send max bytes must be positive")
	}
	if c.RequestMaxBytes <= 0 {
		return errors.New("rpc: request max bytes must be positive")
	}
	if c.MaxConcurrent <= 0 {
		return errors.New("rpc: global concurrency limit must be positive")
	}
	if c.MaxConcurrentPerTenant <= 0 {
		return errors.New("rpc: per-tenant concurrency limit must be positive")
	}
	if c.MaxConcurrentPerTenant > c.MaxConcurrent {
		return errors.New("rpc: per-tenant concurrency limit cannot exceed the global limit")
	}
	if c.MaxDuration <= 0 {
		return errors.New("rpc: max duration must be positive")
	}
	if c.ServerTimeout <= 0 {
		return errors.New("rpc: server timeout must be positive")
	}
	if c.MaxDuration >= c.ServerTimeout {
		return errors.New("rpc: max duration must be shorter than the server timeout")
	}
	if err := validateCORSValues("origin", c.AllowedOrigins); err != nil {
		return err
	}
	return validateCORSValues("header", c.AllowedHeaders)
}

type admission struct {
	closed atomic.Bool
}

func (a *admission) gate(ctx context.Context, _ connect.Spec, _ connect.Peer, _ http.Header) (context.Context, error) {
	if a.closed.Load() {
		return ctx, connect.NewError(connect.CodeUnavailable, errors.New("rpc service is shutting down"))
	}
	return ctx, nil
}

func validateCORSValues(kind string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("rpc: at least one allowed CORS %s is required", kind)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "*" {
			return fmt.Errorf("rpc: allowed CORS %s must be explicit", kind)
		}
		if kind == "origin" && !validOrigin(value) {
			return fmt.Errorf("rpc: allowed CORS origin %q is invalid", value)
		}
		if kind == "header" && !validHeaderName(value) {
			return fmt.Errorf("rpc: allowed CORS header %q is invalid", value)
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("rpc: allowed CORS %s %q is duplicated", kind, value)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validOrigin(value string) bool {
	origin, err := url.Parse(value)
	return err == nil && origin.Scheme != "" && origin.Host != "" && origin.User == nil && origin.Path == "" && origin.RawQuery == "" && origin.Fragment == ""
}

func validHeaderName(value string) bool {
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		if !strings.ContainsRune("!#$%&'*+-.^_`|~", character) {
			return false
		}
	}
	return value != ""
}

type subjectContextKey struct{}

// Subject returns the authenticated subject placed in the context before any
// request message was decoded.
func Subject(ctx context.Context) (auth.Subject, bool) {
	subject, ok := ctx.Value(subjectContextKey{}).(auth.Subject)
	return subject, ok
}

func authenticationGate(authenticate AuthenticateBearer) connect.RequestGateFunc {
	return func(ctx context.Context, _ connect.Spec, _ connect.Peer, header http.Header) (next context.Context, err error) {
		defer func() {
			if recover() != nil {
				next = ctx
				err = connect.NewError(connect.CodeInternal, errors.New("internal error"))
			}
		}()
		token, ok := bearerToken(header.Get("Authorization"))
		if !ok {
			return ctx, connect.NewError(connect.CodeUnauthenticated, errAuthenticationRequired)
		}
		subject, err := authenticate(ctx, token)
		if err != nil || subject.ID == "" || subject.Tenant == "" {
			return ctx, connect.NewError(connect.CodeUnauthenticated, errAuthenticationRequired)
		}
		return context.WithValue(ctx, subjectContextKey{}, subject), nil
	}
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

type callLimiter struct {
	mu           sync.Mutex
	global       int
	globalMax    int
	perTenant    map[string]int
	perTenantMax int
}

func newCallLimiter(globalMax, perTenantMax int) *callLimiter {
	return &callLimiter{
		globalMax:    globalMax,
		perTenant:    make(map[string]int),
		perTenantMax: perTenantMax,
	}
}

func (l *callLimiter) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		release, err := l.acquire(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
		return next(ctx, request)
	}
}

func (l *callLimiter) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (l *callLimiter) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, connection connect.StreamingHandlerConn) error {
		release, err := l.acquire(ctx)
		if err != nil {
			return err
		}
		defer release()
		return next(ctx, connection)
	}
}

func (l *callLimiter) acquire(ctx context.Context) (func(), error) {
	subject, ok := Subject(ctx)
	if !ok || subject.Tenant == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAuthenticationRequired)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	l.mu.Lock()
	if l.global >= l.globalMax || l.perTenant[subject.Tenant] >= l.perTenantMax {
		l.mu.Unlock()
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("concurrent call limit reached"))
	}
	l.global++
	l.perTenant[subject.Tenant]++
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.global--
			l.perTenant[subject.Tenant]--
			if l.perTenant[subject.Tenant] == 0 {
				delete(l.perTenant, subject.Tenant)
			}
			l.mu.Unlock()
		})
	}, nil
}
