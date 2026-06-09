package authcmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/olcf/s3m-cli/internal/auth"
	"github.com/olcf/s3m-cli/internal/cmd"
	"github.com/olcf/s3m-cli/internal/runtime"
	"github.com/olcf/s3m-cli/internal/util"
)

//nolint:funlen,nestif
func BuildLoginCommand(rt *runtime.Runtime) *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Authenticate with S3M",
		Commands: []*cli.Command{
			{
				Name:  "token",
				Usage: "Login with an existing auth token",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "project", Usage: "Project name (required if introspection fails)"},
					&cli.StringFlag{Name: "enclave", Usage: "Security enclave (required if introspection fails)"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if err := rt.EnsureState(); err != nil {
						return cli.Exit(err.Error(), 1)
					}

					//
					// Read and validate input
					token, err := readTokenInput(c.Args().First())
					if err != nil {
						return cli.Exit(err.Error(), 1)
					}

					projectOverride := strings.TrimSpace(c.String("project"))
					enclaveOverride := strings.TrimSpace(c.String("enclave"))

					//
					// Introspect token
					res := auth.IntrospectToken(ctx, rt.Target, token, cmd.GRPCConnectTimeout, cmd.GRPCCallTimeout, rt.Debug)
					rec := res.Record
					rec.Token = token

					//
					// Handle introspection result

					if res.Err != nil {
						fmt.Println(cmd.IntrospectFailNotice)

						rec.LastIntrospectionError = res.Err.Error()
						if rec.Project == "" {
							rec.Project = projectOverride
						}

						if rec.Enclave == "" {
							rec.Enclave = enclaveOverride
						}

						if rec.Project == "" || rec.Enclave == "" {
							return cli.Exit("introspection failed; provide --project and --enclave to store the token", 1)
						}
					} else {
						if projectOverride != "" && !strings.EqualFold(projectOverride, rec.Project) {
							slog.Warn("overriding project from introspection", "project", projectOverride, "introspected", rec.Project)
							rec.Project = projectOverride
						}

						if enclaveOverride != "" && !strings.EqualFold(enclaveOverride, rec.Enclave) {
							slog.Warn("overriding enclave from introspection", "enclave", enclaveOverride, "introspected", rec.Enclave)
							rec.Enclave = enclaveOverride
						}
					}

					//
					// Update metadata

					if rec.IntrospectedAt == nil {
						now := time.Now().UTC()
						rec.IntrospectedAt = &now
					}

					//
					// Store token and state

					if err := rt.State.PutToken(rec); err != nil {
						return cli.Exit(err.Error(), 1)
					}

					if err := auth.SaveState(rt.StatePath, rt.State); err != nil {
						return cli.Exit(err.Error(), 1)
					}

					fmt.Printf("Stored token for enclave %s in project %s\n", rec.Enclave, rec.Project)

					return nil
				},
			},
			{
				Name:   "oidc",
				Usage:  "Login via OIDC flow",
				Action: func(_ context.Context, _ *cli.Command) error { return cli.Exit("not implemented", 1) },
			},
		},
	}
}

//nolint:funlen
func BuildAuthCommand(rt *runtime.Runtime) *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Manage stored auth tokens",
		Commands: []*cli.Command{
			{
				Name:      "enter",
				Usage:     "Switch to a stored enclave/project",
				ArgsUsage: "[enclave] [project]",
				Action: func(_ context.Context, c *cli.Command) error {
					if err := rt.EnsureState(); err != nil {
						return cli.Exit(err.Error(), 1)
					}

					if c.Args().Len() < 2 {
						return cli.Exit("usage: s3m auth enter [enclave] [project]", 1)
					}

					enclave := c.Args().Get(0)
					project := c.Args().Get(1)

					if _, err := rt.State.SwitchContext(enclave, project); err != nil {
						return cli.Exit(err.Error(), 1)
					}

					if err := auth.SaveState(rt.StatePath, rt.State); err != nil {
						return cli.Exit(err.Error(), 1)
					}

					fmt.Printf("Switched to enclave %s, project %s\n", enclave, project)

					return nil
				},
			},
			{
				Name:  "status",
				Usage: "Show stored tokens and current context",
				Action: func(_ context.Context, _ *cli.Command) error {
					if err := rt.EnsureState(); err != nil {
						return cli.Exit(err.Error(), 1)
					}

					curr, ok := rt.State.CurrentToken()
					if !ok {
						fmt.Println("No active token. Login with `s3m login token`.")
					} else {
						fmt.Printf("Current: enclave=%s project=%s scopes=%d [%s]\n",
							curr.Enclave, curr.Project, len(curr.Scopes), summarizeScopes(curr.Scopes))

						if curr.ExpiresAt != nil {
							fmt.Printf("Expires: %s\n", curr.ExpiresAt.Format(time.RFC3339))
						}

						if !curr.IntrospectionOK() {
							fmt.Println("Warning: last introspection failed:", curr.LastIntrospectionError)
						}
					}

					if len(rt.State.Tokens) == 0 {
						fmt.Println("No stored tokens.")
						return nil
					}

					fmt.Println("Stored contexts:")

					records := make([]auth.TokenRecord, 0, len(rt.State.Tokens))
					for _, rec := range rt.State.Tokens {
						records = append(records, rec)
					}

					sort.Slice(records, func(i, j int) bool {
						if records[i].Enclave == records[j].Enclave {
							return records[i].Project < records[j].Project
						}

						return records[i].Enclave < records[j].Enclave
					})

					for _, rec := range records {
						marker := " "
						if ok && strings.EqualFold(rec.Enclave, curr.Enclave) && strings.EqualFold(rec.Project, curr.Project) {
							marker = "*"
						}

						fmt.Printf("%s enclave=%s project=%s scopes=%d [%s]\n",
							marker, rec.Enclave, rec.Project, len(rec.Scopes), summarizeScopes(rec.Scopes))
					}

					return nil
				},
			},
		},
	}
}

func readTokenInput(initial string) (string, error) {
	token := strings.TrimSpace(initial)
	if token != "" {
		return token, nil
	}

	fmt.Print("Token: ")

	fd, err := util.FileDescriptor(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("stdin file descriptor: %w", err)
	}

	raw, err := term.ReadPassword(fd)
	if err != nil {
		return "", err
	}

	fmt.Println()

	token = strings.TrimSpace(string(raw))

	if token == "" {
		return "", errors.New("token is required")
	}

	return token, nil
}

func summarizeScopes(scopes []string) string {
	if len(scopes) == 0 {
		return "none"
	}

	maxScopes := 5

	if len(scopes) <= maxScopes {
		return strings.Join(scopes, ", ")
	}

	rest := len(scopes) - maxScopes

	return fmt.Sprintf("%s +%d more", strings.Join(scopes[:maxScopes], ", "), rest)
}
