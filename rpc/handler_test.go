package rpc_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/rpc"
	fixturev1 "github.com/arandu-io/hesape/rpc/internal/gen/fixture/v1"
	"github.com/arandu-io/hesape/rpc/internal/gen/fixture/v1/fixturev1connect"
)

var defaultConfig = rpc.Config{
	ReadMaxBytes:           1024,
	SendMaxBytes:           1024,
	RequestMaxBytes:        16 * 1024,
	MaxConcurrent:          4,
	MaxConcurrentPerTenant: 2,
	MaxDuration:            time.Second,
	ServerTimeout:          2 * time.Second,
	AllowedOrigins:         []string{"https://app.example"},
	AllowedHeaders:         []string{"Authorization", "Content-Type", "Connect-Protocol-Version", "Connect-Timeout-Ms", "Grpc-Timeout", "X-Grpc-Web", "X-User-Agent"},
}

type fixtureService struct {
	fixturev1connect.UnimplementedCommunicationServiceHandler
	waitForCancellation *atomic.Bool
	panicValue          any
	started             chan struct{}
	release             <-chan struct{}
	protocols           chan<- string
}

func (s *fixtureService) Unary(ctx context.Context, request *connect.Request[fixturev1.EchoRequest]) (*connect.Response[fixturev1.EchoResponse], error) {
	if _, ok := rpc.Subject(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("subject missing"))
	}
	s.recordProtocol(request.Peer().Protocol)
	if s.panicValue != nil {
		panic(s.panicValue)
	}
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.release != nil {
		select {
		case <-ctx.Done():
			if s.waitForCancellation != nil {
				s.waitForCancellation.Store(true)
			}
			return nil, ctx.Err()
		case <-s.release:
		}
	}
	return connect.NewResponse(&fixturev1.EchoResponse{Text: request.Msg.GetText()}), nil
}

func (s *fixtureService) ClientStream(ctx context.Context, stream *connect.ClientStream[fixturev1.EchoRequest]) (*connect.Response[fixturev1.EchoResponse], error) {
	if _, ok := rpc.Subject(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("subject missing"))
	}
	s.recordProtocol(stream.Peer().Protocol)
	var values []string
	for stream.Receive() {
		values = append(values, stream.Msg().GetText())
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return connect.NewResponse(&fixturev1.EchoResponse{Text: strings.Join(values, ",")}), nil
}

func (s *fixtureService) ServerStream(ctx context.Context, request *connect.Request[fixturev1.EchoRequest], stream *connect.ServerStream[fixturev1.EchoResponse]) error {
	if _, ok := rpc.Subject(ctx); !ok {
		return connect.NewError(connect.CodeInternal, errors.New("subject missing"))
	}
	s.recordProtocol(request.Peer().Protocol)
	for _, suffix := range []string{"-1", "-2"} {
		if err := stream.Send(&fixturev1.EchoResponse{Text: request.Msg.GetText() + suffix}); err != nil {
			return err
		}
	}
	return nil
}

func (s *fixtureService) Bidi(ctx context.Context, stream *connect.BidiStream[fixturev1.EchoRequest, fixturev1.EchoResponse]) error {
	if _, ok := rpc.Subject(ctx); !ok {
		return connect.NewError(connect.CodeInternal, errors.New("subject missing"))
	}
	s.recordProtocol(stream.Peer().Protocol)
	for {
		request, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&fixturev1.EchoResponse{Text: request.GetText()}); err != nil {
			return err
		}
	}
}

func (s *fixtureService) recordProtocol(protocol string) {
	if s.protocols != nil {
		s.protocols <- protocol
	}
}

func authenticate(_ context.Context, token string) (auth.Subject, error) {
	if token == "invalid" {
		return auth.Subject{}, errors.New("database exposed secret")
	}
	tenant := "tenant-a"
	if token != "valid" {
		tenant = token
	}
	return auth.Subject{ID: "user-1", Tenant: tenant}, nil
}

func newServer(t *testing.T, service fixturev1connect.CommunicationServiceHandler, config rpc.Config) (*httptest.Server, string) {
	return newServerWithAuthenticator(t, service, config, authenticate)
}

func newServerWithAuthenticator(t *testing.T, service fixturev1connect.CommunicationServiceHandler, config rpc.Config, authenticator rpc.AuthenticateBearer) (*httptest.Server, string) {
	t.Helper()
	path, handler, err := rpc.NewHandler(
		func(options ...connect.HandlerOption) (string, http.Handler) {
			return fixturev1connect.NewCommunicationServiceHandler(service, options...)
		},
		authenticator,
		config,
	)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, path
}

func requestWithToken[T any](message *T, token string) *connect.Request[T] {
	request := connect.NewRequest(message)
	request.Header().Set("Authorization", "Bearer "+token)
	return request
}

func TestAllProtocolsServeAllFourRPCStyles(t *testing.T) {
	protocolsObserved := make(chan string, 12)
	server, _ := newServer(t, &fixtureService{protocols: protocolsObserved}, defaultConfig)
	protocols := []struct {
		name     string
		wireName string
		option   connect.ClientOption
	}{
		{name: "connect", wireName: connect.ProtocolConnect},
		{name: "grpc", wireName: connect.ProtocolGRPC, option: connect.WithGRPC()},
		{name: "grpc-web", wireName: connect.ProtocolGRPCWeb, option: connect.WithGRPCWeb()},
	}

	for _, protocol := range protocols {
		t.Run(protocol.name, func(t *testing.T) {
			var options []connect.ClientOption
			if protocol.option != nil {
				options = append(options, protocol.option)
			}
			client := fixturev1connect.NewCommunicationServiceClient(server.Client(), server.URL, options...)

			unary, err := client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{Text: "unary"}, "valid"))
			if err != nil || unary.Msg.GetText() != "unary" {
				t.Fatalf("unary response = %v, %v", unary, err)
			}

			clientStream := client.ClientStream(t.Context())
			clientStream.RequestHeader().Set("Authorization", "Bearer valid")
			for _, value := range []string{"one", "two"} {
				if err := clientStream.Send(&fixturev1.EchoRequest{Text: value}); err != nil {
					t.Fatal(err)
				}
			}
			clientStreamResponse, err := clientStream.CloseAndReceive()
			if err != nil || clientStreamResponse.Msg.GetText() != "one,two" {
				t.Fatalf("client stream response = %v, %v", clientStreamResponse, err)
			}

			serverStream, err := client.ServerStream(t.Context(), requestWithToken(&fixturev1.EchoRequest{Text: "server"}, "valid"))
			if err != nil {
				t.Fatal(err)
			}
			var serverValues []string
			for serverStream.Receive() {
				serverValues = append(serverValues, serverStream.Msg().GetText())
			}
			if err := serverStream.Err(); err != nil || strings.Join(serverValues, ",") != "server-1,server-2" {
				t.Fatalf("server stream response = %v, %v", serverValues, err)
			}

			bidi := client.Bidi(t.Context())
			bidi.RequestHeader().Set("Authorization", "Bearer valid")
			if err := bidi.Send(&fixturev1.EchoRequest{Text: "bidi"}); err != nil {
				t.Fatal(err)
			}
			if response, err := bidi.Receive(); err != nil || response.GetText() != "bidi" {
				t.Fatalf("bidi response = %v, %v", response, err)
			}
			if err := bidi.CloseRequest(); err != nil {
				t.Fatal(err)
			}
			if _, err := bidi.Receive(); !errors.Is(err, io.EOF) {
				t.Fatalf("bidi close error = %v, want EOF", err)
			}

			for _, style := range []string{"unary", "client-stream", "server-stream", "bidi"} {
				if got := <-protocolsObserved; got != protocol.wireName {
					t.Fatalf("%s reached the server as %q, want %q", style, got, protocol.wireName)
				}
			}
		})
	}
}

func TestAuthenticationRejectsBeforeDecodeAndRedactsResolverErrors(t *testing.T) {
	server, _ := newServer(t, &fixtureService{}, defaultConfig)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+fixturev1connect.CommunicationServiceUnaryProcedure, strings.NewReader("not protobuf"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/proto")
	request.Header.Set("Connect-Protocol-Version", "1")
	request.Header.Set("Authorization", "Bearer invalid")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", response.StatusCode, http.StatusUnauthorized, body)
	}
	if strings.Contains(string(body), "database") || strings.Contains(string(body), "secret") || !strings.Contains(string(body), "authentication required") {
		t.Fatalf("authentication response was not redacted: %s", body)
	}
}

func TestAuthenticationGateRedactsPanics(t *testing.T) {
	server, _ := newServerWithAuthenticator(t, &fixtureService{}, defaultConfig, func(context.Context, string) (auth.Subject, error) {
		panic("token and resolver secret")
	})
	client := fixturev1connect.NewCommunicationServiceClient(server.Client(), server.URL)
	_, err := client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{}, "valid"))
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("code = %v, want internal: %v", connect.CodeOf(err), err)
	}
	if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "resolver") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("authentication panic details escaped: %v", err)
	}
}

func TestPanicsAreRedacted(t *testing.T) {
	server, _ := newServer(t, &fixtureService{panicValue: "token and payload secret"}, defaultConfig)
	client := fixturev1connect.NewCommunicationServiceClient(server.Client(), server.URL)
	_, err := client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{}, "valid"))
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("code = %v, want internal", connect.CodeOf(err))
	}
	if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "payload") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("panic details escaped: %v", err)
	}
}

func TestMaxDurationCancelsApplicationWork(t *testing.T) {
	cancelled := &atomic.Bool{}
	release := make(chan struct{})
	config := defaultConfig
	config.MaxDuration = 30 * time.Millisecond
	server, _ := newServer(t, &fixtureService{waitForCancellation: cancelled, release: release}, config)
	client := fixturev1connect.NewCommunicationServiceClient(server.Client(), server.URL)
	_, err := client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{}, "valid"))
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded {
		t.Fatalf("code = %v, want deadline_exceeded: %v", connect.CodeOf(err), err)
	}
	if !cancelled.Load() {
		t.Fatal("the application did not observe context cancellation")
	}
}

func TestConcurrencyLimitsAreGlobalAndPerTenant(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	config := defaultConfig
	config.MaxConcurrent = 2
	config.MaxConcurrentPerTenant = 1
	server, _ := newServer(t, &fixtureService{started: started, release: release}, config)
	client := fixturev1connect.NewCommunicationServiceClient(server.Client(), server.URL)

	firstDone := make(chan error, 1)
	go func() {
		_, err := client.Unary(context.Background(), requestWithToken(&fixturev1.EchoRequest{}, "valid"))
		firstDone <- err
	}()
	<-started

	_, err := client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{}, "valid"))
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("same-tenant code = %v, want resource_exhausted", connect.CodeOf(err))
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := client.Unary(context.Background(), requestWithToken(&fixturev1.EchoRequest{}, "tenant-b"))
		secondDone <- err
	}()
	<-started
	_, err = client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{}, "third-tenant"))
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("global code = %v, want resource_exhausted", connect.CodeOf(err))
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestCloseAdmissionRefusesNewCallsAndLetsExistingWorkFinish(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	service := &fixtureService{started: started, release: release}
	path, handler, err := rpc.NewHandler(func(options ...connect.HandlerOption) (string, http.Handler) {
		return fixturev1connect.NewCommunicationServiceHandler(service, options...)
	}, authenticate, defaultConfig)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	client := fixturev1connect.NewCommunicationServiceClient(server.Client(), server.URL)

	existingDone := make(chan error, 1)
	go func() {
		_, err := client.Unary(context.Background(), requestWithToken(&fixturev1.EchoRequest{}, "valid"))
		existingDone <- err
	}()
	<-started
	handler.CloseAdmission()

	_, err = client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{}, "valid"))
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("new call code = %v, want unavailable: %v", connect.CodeOf(err), err)
	}
	close(release)
	if err := <-existingDone; err != nil {
		t.Fatalf("existing call was interrupted: %v", err)
	}
}

func TestMessageAndRequestLimitsAreEnforced(t *testing.T) {
	t.Run("read message", func(t *testing.T) {
		config := defaultConfig
		config.ReadMaxBytes = 8
		server, _ := newServer(t, &fixtureService{}, config)
		client := fixturev1connect.NewCommunicationServiceClient(server.Client(), server.URL)
		_, err := client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{Text: strings.Repeat("x", 32)}, "valid"))
		if connect.CodeOf(err) != connect.CodeResourceExhausted {
			t.Fatalf("code = %v, want resource_exhausted: %v", connect.CodeOf(err), err)
		}
	})

	t.Run("send message", func(t *testing.T) {
		config := defaultConfig
		config.SendMaxBytes = 8
		server, _ := newServer(t, &fixtureService{}, config)
		client := fixturev1connect.NewCommunicationServiceClient(server.Client(), server.URL)
		_, err := client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{Text: strings.Repeat("x", 32)}, "valid"))
		if connect.CodeOf(err) != connect.CodeResourceExhausted {
			t.Fatalf("code = %v, want resource_exhausted: %v", connect.CodeOf(err), err)
		}
	})

	t.Run("whole request", func(t *testing.T) {
		config := defaultConfig
		config.RequestMaxBytes = 16
		server, _ := newServer(t, &fixtureService{}, config)
		client := fixturev1connect.NewCommunicationServiceClient(server.Client(), server.URL)
		_, err := client.Unary(t.Context(), requestWithToken(&fixturev1.EchoRequest{Text: strings.Repeat("x", 32)}, "valid"))
		if connect.CodeOf(err) != connect.CodeResourceExhausted {
			t.Fatalf("code = %v, want resource_exhausted: %v", connect.CodeOf(err), err)
		}
	})
}

func TestCORSIsExplicit(t *testing.T) {
	server, path := newServer(t, &fixtureService{}, defaultConfig)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, server.URL+path+"Unary", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "https://app.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || response.Header.Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("preflight = %d, origin %q", response.StatusCode, response.Header.Get("Access-Control-Allow-Origin"))
	}

	request, err = http.NewRequestWithContext(t.Context(), http.MethodOptions, server.URL+path+"Unary", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("disallowed origin status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
}

func TestCORSLeavesRequestsOutsideTheServiceBoundaryAsNotFound(t *testing.T) {
	path, handler, err := rpc.NewHandler(func(options ...connect.HandlerOption) (string, http.Handler) {
		return fixturev1connect.NewCommunicationServiceHandler(&fixtureService{}, options...)
	}, authenticate, defaultConfig)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodOptions, "https://rpc.example/fixture.v1.CommunicationService.evil/Unary", nil)
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("neighbor outside %q returned %d, want %d", path, response.Code, http.StatusNotFound)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS headers were added outside the registered service boundary")
	}
}

func TestConfigurationRequiresEveryLimitAndExplicitCORS(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*rpc.Config)
	}{
		{name: "read", mutate: func(config *rpc.Config) { config.ReadMaxBytes = 0 }},
		{name: "send", mutate: func(config *rpc.Config) { config.SendMaxBytes = 0 }},
		{name: "request", mutate: func(config *rpc.Config) { config.RequestMaxBytes = 0 }},
		{name: "global concurrency", mutate: func(config *rpc.Config) { config.MaxConcurrent = 0 }},
		{name: "tenant concurrency", mutate: func(config *rpc.Config) { config.MaxConcurrentPerTenant = 0 }},
		{name: "duration", mutate: func(config *rpc.Config) { config.MaxDuration = 0 }},
		{name: "server timeout", mutate: func(config *rpc.Config) { config.ServerTimeout = config.MaxDuration }},
		{name: "origin wildcard", mutate: func(config *rpc.Config) { config.AllowedOrigins = []string{"*"} }},
		{name: "header wildcard", mutate: func(config *rpc.Config) { config.AllowedHeaders = []string{"*"} }},
		{name: "origin path", mutate: func(config *rpc.Config) { config.AllowedOrigins = []string{"https://app.example/path"} }},
		{name: "invalid header", mutate: func(config *rpc.Config) { config.AllowedHeaders = []string{"Authorization\nInjected"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := defaultConfig
			test.mutate(&config)
			_, _, err := rpc.NewHandler(func(options ...connect.HandlerOption) (string, http.Handler) {
				return fixturev1connect.NewCommunicationServiceHandler(&fixtureService{}, options...)
			}, authenticate, config)
			if err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}
