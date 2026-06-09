package execcmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/proto"
)

func paramCompleter(params []proto.ParamSpec) cli.ShellCompleteFunc {
	return func(ctx context.Context, c *cli.Command) {
		args := c.Args().Slice()
		if len(args) > 0 && strings.HasPrefix(args[len(args)-1], "-") {
			cli.DefaultCompleteWithFlags(ctx, c)

			return
		}

		provided, lastArg := parseProvidedParams(args)
		prefix := extractPrefix(lastArg)

		for _, p := range params {
			if _, done := provided[p.Name]; done {
				continue
			}

			if prefix != "" && !strings.HasPrefix(p.Name, prefix) {
				continue
			}

			printParamCompletions(c.Root().Writer, p)
		}
	}
}

func parseProvidedParams(args []string) (provided map[string]struct{}, lastArg string) {
	provided = map[string]struct{}{}

	if len(args) > 0 {
		lastArg = args[len(args)-1]
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}

		if name, value, ok := strings.Cut(arg, "="); ok {
			if arg == lastArg && value == "" {
				continue
			}

			provided[name] = struct{}{}
		}
	}

	return provided, lastArg
}

func extractPrefix(lastArg string) string {
	prefix := lastArg

	if idx := strings.Index(prefix, "="); idx >= 0 {
		if idx == len(prefix)-1 {
			prefix = prefix[:idx]
		} else {
			prefix = ""
		}
	}

	return prefix
}

func printParamCompletions(w io.Writer, p proto.ParamSpec) {
	if len(p.AllowedValues) > 0 {
		for _, val := range p.AllowedValues {
			_, _ = fmt.Fprintln(w, p.Name+"="+val)
		}

		_, _ = fmt.Fprintln(w, p.Name+"=")

		return
	}

	_, _ = fmt.Fprintln(w, p.Name+"=")
}

func execGroupCompleter(cmds []*cli.Command) cli.ShellCompleteFunc {
	return func(ctx context.Context, c *cli.Command) {
		args := c.Args().Slice()
		if len(args) > 0 && strings.HasPrefix(args[len(args)-1], "-") {
			cli.DefaultCompleteWithFlags(ctx, c)

			return
		}

		seen := map[string]struct{}{}
		w := c.Root().Writer

		for _, sc := range cmds {
			if sc == nil || sc.Hidden {
				continue
			}

			if len(sc.Aliases) > 0 {
				alias := sc.Aliases[0]
				if _, ok := seen[alias]; !ok {
					_, _ = fmt.Fprintln(w, alias)
					seen[alias] = struct{}{}
				}
			}

			if _, ok := seen[sc.Name]; !ok {
				_, _ = fmt.Fprintln(w, sc.Name)
				seen[sc.Name] = struct{}{}
			}
		}
	}
}
