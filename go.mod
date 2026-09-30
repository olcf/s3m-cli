module github.com/olcf/s3m-cli

go 1.27.1

require (
	github.com/jedib0t/go-pretty/v6 v6.8.3
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/olcf/s3m-apis v0.4.0
	github.com/urfave/cli/v3 v3.13.0
	golang.org/x/term v0.46.0
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.31.0 // indirect
	github.com/mattn/go-runewidth v0.0.30 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260921155816-b14227669459 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260921155816-b14227669459 // indirect
)

replace github.com/olcf/s3m-apis => ./internal/pkg/s3m-apis
