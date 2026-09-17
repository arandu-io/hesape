module github.com/arandu-io/hesape/rpc

go 1.26

require (
	connectrpc.com/connect v1.21.0
	github.com/arandu-io/hesape v0.41.2
)

require google.golang.org/protobuf v1.36.11

tool (
	connectrpc.com/connect/cmd/protoc-gen-connect-go
	google.golang.org/protobuf/cmd/protoc-gen-go
)
