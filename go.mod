module github.com/olcf/s3m-cli

go 1.26.4

require (
	github.com/jedib0t/go-pretty/v6 v6.8.0
	github.com/modelcontextprotocol/go-sdk v1.6.1
	github.com/olcf/s3m-apis v0.0.0-00010101000000-000000000000
	github.com/urfave/cli/v3 v3.9.0
	golang.org/x/term v0.44.0
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260608224507-4308a22a1bab // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260608224507-4308a22a1bab // indirect
)

replace github.com/olcf/s3m-apis => ./internal/pkg/s3m-apis
