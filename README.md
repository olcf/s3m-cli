# s3m-cli

Command-line client for S3M, the service network that provides programmatic access to HPC resources at the Oak Ridge Leadership Computing Facility (OLCF). It connects to the S3M API over gRPC, and can also run as a server that exposes S3M's endpoints to AI assistants through the Model Context Protocol (MCP) or to HTTP clients as an OpenAPI server.

The commands under `s3m exec`, the MCP tools, and the OpenAPI spec are all generated from a [s3m-apis](https://github.com/olcf/s3m-apis) protobuf descriptor set. At startup, the CLI walks that set and turns every non-streaming RPC into a subcommand.

## Installing

Build it manually:

```sh
git clone https://github.com/olcf/s3m-cli
cd s3m-cli
make build
```

The binary is written to `binaries/s3m`; move it into your `PATH`.

## Logging in

Authenticate with an S3M token issued for one of your projects in [myOLCF](https://my.olcf.ornl.gov/) (paste at the prompt):

```sh
s3m login token
```

The input prompt keeps the token off your screen; you can also pass it as an argument if you're on a personal machine where having the token in your shell history is safe:

```sh
s3m login token <token>
```

On login, the CLI introspects the token to learn its project, enclave, and granted scopes. If introspection can't reach S3M, pass `--project` and `--enclave` to store the token anyway; until the token can be introspected, the CLI exposes every RPC and tool and lets the gateway reject anything you're not authorized for. Otherwise, it only exposes RPCs and tools for the endpoints your scopes grant.

Token logins are saved per enclave and project, so you can hold several and switch between them:

```sh
s3m auth status
s3m auth enter <enclave> <project>
```

OIDC login via `s3m login oidc` is coming soon.


-----

## Calling RPCs

`s3m exec` groups the RPCs by API and version; run a group on its own to see its calls:

```sh
s3m exec                  # list API groups (slurm/v0044, storage/v1alpha, ...)
s3m exec storage/v1alpha  # list the calls in one group
```

Each call has a long name like `bucket-gateway--list-datasets` and a shorter alias when it's unambiguous (typical):

```sh
s3m exec storage/v1alpha list-datasets
```

Pass request fields as `field=value` arguments. Use dots for nested fields, and prefix a value with `@` to read it from a file or `@-` to read stdin. Some calls also need a header parameter, which you pass the same way; `--help` on a call lists its fields, any required headers, and whether your token grants access.

```sh
s3m exec slurm/v0044 get-partitions compute_resource=defiant
```

For a large request body, skip the field arguments and pass JSON with `--data` (`@file`, or `-` for stdin):

```sh
s3m exec slurm/v0044 post-job-submit --data @job.json
```

Format output as human-readable text (`-o text`, default) or JSON (`-o json`).

## Storage commands

The CLI includes dedicated commands for S3M Storage, S3M's bucket service intended for secure short-term data storage and transfer.

```sh
s3m storage ls                                    # list active datasets
s3m storage ls my-dataset                         # list files in a dataset
s3m storage ls my-dataset '*.csv'                 # glob (search) in a dataset
s3m storage push my-dataset ./results             # upload a file or directory
s3m storage pull my-dataset '**/*.parquet' ./out  # pull contents of a dataset
s3m storage delete my-dataset                     # delete a dataset
```

Refer to a dataset by name (add `--latest` to pick the most recent ready one) or by ID with `--id`. A unique prefix of an ID works too. Note: datasets are immutable once created; additional files cannot be added via `push` later, and contents cannot be edited, renamed, moved, or individually deleted.

Patterns containing `*`, `?`, or `[` are treated as globs. Otherwise, patterns are matched as path prefixes.


-----

## Running an MCP or OpenAPI server

You can use the CLI to serve MCP and OpenAPI servers for AI clients and agents, translating their requests to gRPC calls to the S3M API.

By default, `s3m mcp` exposes MCP over stdio, which is what local AI clients like Claude Desktop and Claude Code expect. MCP can also use `http` transport. OpenAPI always serves via HTTP and publishes its spec at `/openapi.json`.

```sh
s3m mcp [--http] [--http-addr 127.0.0.1:5310]
s3m openapi [--addr 127.0.0.1:8080]
```

The default is "stateful auth" mode, which re-uses your token from `s3m login`. In this mode, the CLI only exposes tools for RPCs your token can access.

To serve over HTTP and take the S3M token from each incoming request's `Authorization` header instead of authenticating with the stored CLI token (useful for shared deployments of the MCP server):

```sh
s3m mcp --http --stateless-auth
```

The S3M CLI also serves documentation tools via MCP and OpenAPI, to help AI agents understand how to use the other tools. These are populated from a docs archive fetched over HTTP (`--docs-url`, default the public `olcf/s3m-mcp-docs` archive).


## Configuration

The S3M gRPC endpoint defaults to `s3m.olcf.ornl.gov:443` over TLS; override it with the global `--grpc-target`, and add `--debug` for verbose logging. Pass `--ignore-permissions` to expose every RPC and tool regardless of your token's granted scopes, leaving the gateway to reject anything you're not authorized for.

Login state is written to `auth.json` under your platform's data directory (`~/Library/Application Support/s3m` on macOS, `~/.local/share/s3m` on Linux, `%LocalAppData%\s3m` on Windows) with `0600` permissions. A few environment variables change the defaults:

| Variable                                | Effect                                                                                                      |
|-----------------------------------------|-------------------------------------------------------------------------------------------------------------|
| `S3M_AUTH_PATH`                         | Overrides the path to the login state file                                                                  |
| `XDG_DATA_HOME`                         | Base directory for the default state path                                                                   |
| `S3MIO_PULL_TOKEN` / `S3MIO_PUSH_TOKEN` | Read and write tokens for storage, used as a fallback when there's no stored login (compute jobs set these) |
| `S3M_DOCS_TOKEN`                        | Access token for fetching the docs archive                                                                  |

For storage commands, an explicit `--token` wins, then the matching `S3MIO_*_TOKEN`, then your stored login.


## Shell completions

Load completions for the current shell session:

| Shell      | Command                                              |
|------------|------------------------------------------------------|
| Bash       | `source <(s3m completion bash)`                      |
| Zsh        | `source <(s3m completion zsh)`                       |
| Fish       | `s3m completion fish \| source`                      |
| PowerShell | `s3m completion pwsh \| Invoke-Expression`           |


-----

## License

Except where otherwise noted, the contents of this repository are available under either of the following licenses, at your option:

- [Apache License 2.0](LICENSE-APACHE)
- [MIT License](LICENSE-MIT)

Vendored third-party dependencies under `vendor/` are distributed under their own licenses, retained alongside their source.
