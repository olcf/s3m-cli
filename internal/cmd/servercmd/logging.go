package servercmd

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func safeStringAttr(key, value string) slog.Attr {
	return slog.String(key, sanitizeLogValue(value))
}

func sanitizeLogValue(value string) string {
	if value == "" {
		return ""
	}

	value = strings.ReplaceAll(value, "\r", `\r`)
	value = strings.ReplaceAll(value, "\n", `\n`)

	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}

		return r
	}, value)
}

func truncateForLog(value string) string {
	if len(value) <= 8 {
		return sanitizeLogValue(value)
	}

	return sanitizeLogValue(value[:8]) + "..."
}

func logAuthTokenExtraction(r *http.Request, token string) {
	slog.LogAttrs(r.Context(), slog.LevelDebug, "wrapWithAuthTokenExtraction",
		safeStringAttr("method", r.Method),
		safeStringAttr("path", r.URL.Path),
		slog.Bool("hasToken", token != ""),
		safeStringAttr("tokenPrefix", truncateForLog(token)),
	)
}

func logMissingAuthorization(r *http.Request) {
	slog.LogAttrs(r.Context(), slog.LevelWarn, "Rejecting request: missing Authorization header",
		safeStringAttr("path", r.URL.Path),
	)
}

func logMCPServerFactory(r *http.Request, token string) {
	slog.LogAttrs(r.Context(), slog.LevelDebug, "MCP server factory called",
		slog.Bool("hasToken", token != ""),
		safeStringAttr("tokenPrefix", truncateForLog(token)),
		safeStringAttr("method", r.Method),
		safeStringAttr("path", r.URL.Path),
	)
}

func logMCPToolHandler(ctx context.Context, req *mcp.CallToolRequest, token string) {
	slog.LogAttrs(ctx, slog.LevelDebug, "MCP tool handler invoked",
		safeStringAttr("tool", req.Params.Name),
		slog.Bool("hasToken", token != ""),
		safeStringAttr("tokenPrefix", truncateForLog(token)),
	)
}
