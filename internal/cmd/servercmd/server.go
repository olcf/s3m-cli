package servercmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"

	buildinfo "github.com/olcf/s3m-cli"
	"github.com/olcf/s3m-cli/internal/cmd"
	"github.com/olcf/s3m-cli/internal/gitdocs"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/proto"
	"github.com/olcf/s3m-cli/internal/runtime"
	"github.com/olcf/s3m-cli/internal/toolset"
)

//nolint:funlen,gocognit
func BuildMCPCommand(rt *runtime.Runtime) *cli.Command {
	flags := append([]cli.Flag{
		&cli.BoolFlag{Name: "http",
			Usage: "Also serve MCP over HTTP"},
		&cli.StringFlag{Name: "http-addr", Value: cmd.DefaultMCPHTTPAddr,
			Usage: "Address for MCP HTTP mode"},
		&cli.BoolFlag{Name: "stateless-auth",
			Usage: "Use S3M token from Authorization header for MCP over HTTP and OpenAPI servers"},
		&cli.BoolFlag{Name: "stdio",
			Usage: "Also serve MCP over STDIO (default when --http is not set)"},
	}, docFlags()...)

	return &cli.Command{
		Name:  "mcp",
		Usage: "Run the MCP server (STDIO by default)",
		Flags: flags,
		Action: func(ctx context.Context, c *cli.Command) error {
			stateless := c.Bool("stateless-auth")
			httpMode := c.Bool("http")
			stdioMode := c.Bool("stdio")

			// default: STDIO only when --http not specified
			if !httpMode && !stdioMode {
				stdioMode = true
			}

			// Enable info-level logging for HTTP-only servers (STDIO mode stays quiet)
			if httpMode && !stdioMode && !rt.Debug {
				handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
				slog.SetDefault(slog.New(handler))
			}

			docsCtx, cancelDocs := context.WithCancel(ctx)
			defer cancelDocs()

			if err := configureGitDocs(docsCtx, rt, c); err != nil {
				slog.Warn("git docs init failed, docs unavailable", "error", err)
			}

			slog.Info("starting MCP server",
				"grpcTarget", rt.Target,
				"docs", docCount(rt))

			// Per-request filtered servers in stateless mode
			if stateless && httpMode {
				conn, err := grpcclient.DialAndWait(ctx, rt.Target, "", cmd.GRPCConnectTimeout, rt.Debug)
				if err != nil {
					return cli.Exit(err.Error(), 1)
				}

				defer closeConn(conn)

				cache := newTokenCache(rt)
				handler := newStatelessMCPHTTPHandler(rt, conn, cache)

				addr := c.String("http-addr")
				slog.Info("listening for MCP HTTP (stateless, per-token filtering)", "addr", addr)

				srv := &http.Server{
					Addr:              addr,
					Handler:           handler,
					ReadHeaderTimeout: 10 * time.Second,
					ReadTimeout:       30 * time.Second,
					WriteTimeout:      30 * time.Second,
					IdleTimeout:       60 * time.Second,
				}

				// no STDIO in stateless mode
				return srv.ListenAndServe()
			}

			// Non-stateless mode: single shared server
			allowed, conn, err := prepareConnection(ctx, rt)
			if err != nil {
				return err
			}

			defer closeConn(conn)

			cache := newTokenCache(rt)

			server := mcp.NewServer(&mcp.Implementation{
				Name:    "s3m",
				Version: buildinfo.Version,
			}, nil)

			ts := buildToolSet(rt, allowed, conn, cache.Vars)

			for _, spec := range ts.MCP {
				server.AddTool(spec.Tool, spec.Handler)
			}

			if httpMode {
				addr := c.String("http-addr")

				handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
					return server
				}, &mcp.StreamableHTTPOptions{
					Stateless:      true,
					SessionTimeout: cmd.MCPSessionTimeout,
				})

				slog.Info("listening for MCP HTTP", "addr", addr)

				srv := &http.Server{
					Addr:              addr,
					Handler:           handler,
					ReadHeaderTimeout: 10 * time.Second,
					ReadTimeout:       30 * time.Second,
					WriteTimeout:      30 * time.Second,
					IdleTimeout:       60 * time.Second,
				}

				if stdioMode {
					// run HTTP in background, STDIO in foreground
					go func() {
						if err := srv.ListenAndServe(); err != nil {
							slog.Error("MCP HTTP server exited", "error", err)
						}
					}()
				} else {
					// HTTP-only: block on HTTP server
					return srv.ListenAndServe()
				}
			}

			if stdioMode {
				slog.Info("serving MCP over stdio")
				return server.Run(ctx, &mcp.StdioTransport{})
			}

			return nil
		},
	}
}

func newStatelessMCPHTTPHandler(
	rt *runtime.Runtime,
	conn *grpc.ClientConn,
	cache *tokenCache,
) http.Handler {
	var handler http.Handler = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		token := grpcclient.TokenFromAuthorizationHeader(r.Header.Get("Authorization"))

		logMCPServerFactory(r, token)

		perms := cache.GetPermissions(r.Context(), token)
		allowed := filterAllowed(perms, rt.Methods)

		ts := buildToolSet(rt, allowed, conn, cache.Vars)

		srv := mcp.NewServer(&mcp.Implementation{
			Name:    "s3m",
			Version: buildinfo.Version,
		}, nil)

		for _, spec := range ts.MCP {
			// Wrap handler to inject auth token into context.
			// The MCP framework doesn't propagate HTTP request context values to tool handlers,
			// so we need to explicitly inject the token for gRPC calls to be authenticated.
			originalHandler := spec.Handler
			capturedToken := token // explicit capture for closure
			wrappedHandler := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				logMCPToolHandler(ctx, req, capturedToken)

				ctx = grpcclient.ContextWithAuthToken(ctx, capturedToken)

				return originalHandler(ctx, req)
			}
			srv.AddTool(spec.Tool, wrappedHandler)
		}

		return srv
	}, &mcp.StreamableHTTPOptions{
		Stateless:      true,
		SessionTimeout: cmd.MCPSessionTimeout,
	})

	return wrapWithAuthTokenExtraction(handler)
}

//nolint:funlen
func BuildOpenAPICommand(rt *runtime.Runtime) *cli.Command {
	flags := append([]cli.Flag{
		&cli.StringFlag{Name: "addr", Value: cmd.DefaultOpenAPIAddr, Usage: "Address to bind"},
		&cli.BoolFlag{Name: "stateless-auth",
			Usage: "Use stateless auth: require S3M token from Authorization header and do not require a stored login"},
	}, docFlags()...)

	return &cli.Command{
		Name:  "openapi",
		Usage: "Run the OpenAPI server",
		Flags: flags,
		Action: func(ctx context.Context, c *cli.Command) error {
			stateless := c.Bool("stateless-auth")

			docsCtx, cancelDocs := context.WithCancel(ctx)
			defer cancelDocs()

			if err := configureGitDocs(docsCtx, rt, c); err != nil {
				slog.Warn("git docs init failed, docs unavailable", "error", err)
			}

			var (
				allowed []proto.MethodInfo
				conn    *grpc.ClientConn
				err     error
			)

			if stateless {
				allowed, conn, err = prepareStatelessConnection(ctx, rt)
			} else {
				allowed, conn, err = prepareConnection(ctx, rt)
			}

			if err != nil {
				return err
			}

			defer closeConn(conn)

			cache := newTokenCache(rt)
			ts := buildToolSet(rt, allowed, conn, cache.Vars)

			mux := http.NewServeMux()
			if stateless {
				mux.HandleFunc("/openapi.json", makeStatelessOpenAPISpecHandler(rt, conn, cache))
				registerStatelessOpenAPIHandlers(mux, ts.HTTP, rt, conn, cache)
			} else {
				spec := toolset.GenerateOpenAPISpec(ts.OpenAPI)

				mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodOptions {
						toolset.WriteCORSHeaders(w, r)
						w.WriteHeader(http.StatusNoContent)

						return
					}

					toolset.WriteCORSHeaders(w, r)
					w.Header().Set("Content-Type", "application/json")

					if err := json.NewEncoder(w).Encode(spec); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				})
				toolset.RegisterOpenAPIHandlers(mux, ts.HTTP)
			}

			addr := c.String("addr")
			slog.Info("serving OpenAPI", "addr", addr)

			srv := &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      30 * time.Second,
				IdleTimeout:       60 * time.Second,
			}

			return srv.ListenAndServe()
		},
	}
}

func configureGitDocs(ctx context.Context, rt *runtime.Runtime, c *cli.Command) error {
	docsURL := c.String("docs-url")
	if docsURL == "" {
		return nil
	}

	return initGitDocs(ctx, rt, &gitdocs.Config{
		URL:   docsURL,
		Token: c.String("docs-token"),
		Poll:  c.Duration("docs-poll"),
		Path:  c.String("docs-path"),
	})
}

func docFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "docs-url", Value: defaultDocsArchiveURL,
			Usage: "Archive URL for git-hosted docs"},
		&cli.StringFlag{Name: "docs-token",
			Sources: cli.EnvVars("S3M_DOCS_TOKEN"),
			Usage:   "Optional access token for docs archive fetches"},
		&cli.DurationFlag{Name: "docs-poll", Value: defaultDocsPoll,
			Usage: "Poll interval for docs (0=disable)"},
		&cli.StringFlag{Name: "docs-path", Value: defaultDocsPath,
			Usage: "Subdirectory in the docs archive containing markdown docs"},
	}
}

func docCount(rt *runtime.Runtime) int {
	if rt == nil {
		return 0
	}

	store := rt.GetDocs()
	if store == nil {
		return 0
	}

	return len(store.Docs)
}
