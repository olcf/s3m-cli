package cmd

import "time"

const (
	DefaultS3MEndpoint = "s3m.olcf.ornl.gov:443"

	DefaultMCPHTTPAddr = "127.0.0.1:5310"
	DefaultOpenAPIAddr = "127.0.0.1:8080"

	OutputFormatJSON = "json"
	OutputFormatText = "text"

	IntrospectFailNotice = "" +
		"Token introspection failed. The token can still be stored, but MCP/OpenAPI " +
		"tool exposure and dynamic exec access will stay conservative until scopes can " +
		"be introspected."
)

const (
	GRPCCallTimeout    = 30 * time.Second
	GRPCConnectTimeout = 10 * time.Second
	MCPSessionTimeout  = 10 * time.Minute
)
