package execcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/urfave/cli/v3"

	"github.com/olcf/s3m-cli/internal/cmd"
	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/permissions"
	"github.com/olcf/s3m-cli/internal/proto"
	"github.com/olcf/s3m-cli/internal/runtime"
)

func BuildExecCommand(rt *runtime.Runtime) *cli.Command {
	perms := rt.PermissionSnapshot()
	groups := groupMethods(rt.Methods)
	sub := make([]*cli.Command, 0, len(groups))

	for _, g := range groups {
		sub = append(sub, buildExecGroup(rt, g, perms))
	}

	return &cli.Command{
		Name:     "exec",
		Usage:    "Call S3M RPCs dynamically",
		Commands: sub,
	}
}

type methodGroup struct {
	Name    string
	Methods []proto.MethodInfo
}

func groupMethods(methods []proto.MethodInfo) []methodGroup {
	grouped := map[string][]proto.MethodInfo{}

	for _, m := range methods {
		key := fmt.Sprintf("%s/%s", strings.ToLower(m.API), strings.ToLower(m.Version))
		grouped[key] = append(grouped[key], m)
	}

	keys := make([]string, 0, len(grouped))

	for k := range grouped {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	out := make([]methodGroup, 0, len(keys))

	for _, k := range keys {
		ms := grouped[k]
		sort.Slice(ms, func(i, j int) bool {
			if ms[i].Service.FullName() == ms[j].Service.FullName() {
				return ms[i].Method.Name() < ms[j].Method.Name()
			}

			return ms[i].Service.FullName() < ms[j].Service.FullName()
		})

		out = append(out, methodGroup{Name: k, Methods: ms})
	}

	return out
}

func buildExecGroup(rt *runtime.Runtime, grp methodGroup, perms permissions.Snapshot) *cli.Command {
	aliasCounts := make(map[string]int)

	for _, m := range grp.Methods {
		alias := slug(string(m.Method.Name()))
		aliasCounts[alias]++
	}

	cmds := make([]*cli.Command, 0, len(grp.Methods))
	for _, m := range grp.Methods {
		cmds = append(cmds, buildExecMethodCommand(rt, m, perms, aliasCounts))
	}

	return &cli.Command{
		Name:          grp.Name,
		Usage:         "Calls for " + grp.Name,
		Commands:      cmds,
		ShellComplete: execGroupCompleter(cmds),
	}
}

func buildExecMethodCommand(
	rt *runtime.Runtime, m proto.MethodInfo, perms permissions.Snapshot, aliasCounts map[string]int,
) *cli.Command {
	name := slug(string(m.Service.Name()) + "-" + string(m.Method.Name()))
	alias := slug(string(m.Method.Name()))

	var aliases []string

	if aliasCounts[alias] == 1 {
		aliases = append(aliases, alias)
	}

	status := perms.Label(m)
	usage := fmt.Sprintf("%s (%s)", m.Desc, status)

	if len(m.Headers) > 0 {
		parts := make([]string, 0, len(m.Headers))

		for _, h := range m.Headers {
			label := h.ParamName
			if len(h.AllowedValues) > 0 {
				label += fmt.Sprintf(" (allowed: %s)", strings.Join(h.AllowedValues, ", "))
			}

			if h.Required {
				label += " [required]"
			}

			parts = append(parts, label)
		}

		usage = fmt.Sprintf("%s | header params: %s", usage, strings.Join(parts, "; "))
	}

	params := proto.ParamsForMethod(m.Method, m.Headers)

	return &cli.Command{
		Name:      name,
		Aliases:   aliases,
		Usage:     usage,
		ArgsUsage: "[param=value ...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "data", Aliases: []string{"d"},
				Usage: "JSON request payload. Use @path or '-' for stdin."},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: cmd.OutputFormatText,
				Usage: "Output format: text or json"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return runExecMethod(ctx, c, rt, m)
		},
		ShellComplete: paramCompleter(params),
	}
}

//nolint:funlen
func runExecMethod(ctx context.Context, c *cli.Command, rt *runtime.Runtime, m proto.MethodInfo) error {
	output := strings.ToLower(c.String("output"))
	if output != cmd.OutputFormatJSON && output != cmd.OutputFormatText {
		return cli.Exit("output must be 'json' or 'text'", 1)
	}

	//
	// Authenticate and authorize

	if err := rt.EnsureState(); err != nil {
		return formatError(output, err)
	}

	tokenRec, ok := rt.CurrentToken()
	if !ok {
		return formatError(output, errors.New("no active token; login with `s3m login token`"))
	}

	perms := rt.PermissionSnapshot()

	if perms.Known && !perms.Can(m) {
		msg := fmt.Sprintf("permission denied for %s: this token does not grant access to this RPC. "+
			"Check `s3m auth status` or use a token that grants access.", m.Method.FullName())

		return formatError(output, errors.New(msg))
	}

	//
	// Prepare request payload

	args := c.Args().Slice()
	dataSpec := c.String("data")

	if dataSpec != "" && len(args) > 0 {
		return formatError(output, errors.New("provide either --data or param=value arguments, not both"))
	}

	var payload []byte

	var err error
	if len(args) > 0 {
		payload, err = payloadFromArgs(args)
	} else {
		payload, err = readPayload(dataSpec)
	}

	if err != nil {
		return formatError(output, err)
	}

	bodyJSON, headerVals, err := proto.ExtractHeadersAndBody(payload, m.Headers)
	if err != nil {
		return formatError(output, err)
	}

	//
	// Establish connection

	conn, err := grpcclient.DialAndWait(ctx, rt.Target, tokenRec.Token, cmd.GRPCConnectTimeout, rt.Debug)
	if err != nil {
		return formatError(output, err)
	}

	defer func() {
		if err := conn.Close(); err != nil {
			slog.Warn("failed to close connection", "error", err)
		}
	}()

	//
	// Invoke RPC

	key := grpcclient.MethodKey{
		ServiceFull: string(m.Service.FullName()),
		Method:      string(m.Method.Name()),
	}

	respJSON, err := grpcclient.InvokeJSON(
		ctx, bodyJSON, m.Method, conn, key, headerVals, cmd.GRPCCallTimeout, rt.Debug)
	if err != nil {
		return formatError(output, err)
	}

	//
	// Output response

	return printOutput(output, respJSON)
}

func payloadFromArgs(args []string) ([]byte, error) {
	payload := make(map[string]any, len(args))
	stdinUsed := false

	for _, arg := range args {
		key, val, ok := strings.Cut(arg, "=")
		if !ok {
			return nil, fmt.Errorf("invalid argument %q: expected key=value", arg)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid argument %q: missing parameter name", arg)
		}

		if val == "@-" {
			if stdinUsed {
				return nil, errors.New("repeated @- is not allowed: stdin can only be used once per exec request")
			}

			stdinUsed = true
		}

		parsed, err := parseValueSpec(val)
		if err != nil {
			return nil, err
		}

		if err := insertParam(payload, key, parsed); err != nil {
			return nil, err
		}
	}

	return json.Marshal(payload)
}

func readPayload(spec string) ([]byte, error) {
	spec = strings.TrimSpace(spec)
	fromTerminal := isInputFromTerminal()

	if spec == "" {
		if fromTerminal {
			return nil, nil
		}

		return io.ReadAll(os.Stdin)
	}

	switch {
	case spec == "-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(spec, "@"):
		return os.ReadFile(strings.TrimPrefix(spec, "@"))
	default:
		return []byte(spec), nil
	}
}

func parseValueSpec(spec string) (any, error) {
	if path, ok := strings.CutPrefix(spec, "@"); ok {
		if path == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil, fmt.Errorf("read stdin: %w", err)
			}

			return string(data), nil
		}

		// #nosec G304
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read file %q: %w", path, err)
		}

		return string(data), nil
	}

	var parsed any
	if err := json.Unmarshal([]byte(spec), &parsed); err == nil {
		return parsed, nil
	}

	return spec, nil
}

func insertParam(dst map[string]any, key string, value any) error {
	parts := strings.Split(key, ".")

	curr := dst

	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("invalid parameter name %q", key)
		}

		if i == len(parts)-1 {
			if _, exists := curr[part]; exists {
				return fmt.Errorf("duplicate parameter %q", key)
			}

			curr[part] = value

			return nil
		}

		next, ok := curr[part]
		if !ok {
			child := map[string]any{}
			curr[part] = child
			curr = child

			continue
		}

		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("parameter %q conflicts with already set %q", key, strings.Join(parts[:i+1], "."))
		}

		curr = child
	}

	return nil
}

func isInputFromTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return (info.Mode() & os.ModeCharDevice) != 0
}

func formatError(output string, err error) error {
	if output == cmd.OutputFormatJSON {
		payload, encErr := json.Marshal(map[string]string{"error": err.Error()})
		if encErr != nil {
			slog.Error("failed to marshal error", "error", encErr)

			return cli.Exit(err.Error(), 1)
		}

		fmt.Println(string(payload))

		return cli.Exit("", 1)
	}

	return cli.Exit(err.Error(), 1)
}

func printOutput(output string, data []byte) error {
	switch output {
	case cmd.OutputFormatJSON:
		fmt.Println(string(data))
	case cmd.OutputFormatText:
		var obj any
		if err := json.Unmarshal(data, &obj); err != nil {
			fmt.Println(string(data))

			return err
		}

		pretty, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			fmt.Println(string(data))

			return err
		}

		fmt.Println(string(pretty))
	default:
		return cli.Exit("unknown output format", 1)
	}

	return nil
}

//nolint:cyclop
func slug(text string) string {
	if text == "" {
		return ""
	}

	var (
		runes     = []rune(text)
		prevSpace = true

		b strings.Builder
	)

	for i, r := range runes {
		switch {
		case r == '_' || r == '.' || unicode.IsSpace(r):
			if !prevSpace {
				b.WriteRune(' ')

				prevSpace = true
			}

			continue
		case unicode.IsUpper(r):
			var prev rune
			if i > 0 {
				prev = runes[i-1]
			}

			var next rune
			if i+1 < len(runes) {
				next = runes[i+1]
			}

			prevIsUpper := i > 0 && unicode.IsUpper(prev)
			prevIsLower := i > 0 && unicode.IsLower(prev)
			prevIsDigit := i > 0 && unicode.IsDigit(prev)
			prevIsDash := prev == '-'

			nextIsLower := unicode.IsLower(next)
			nextIsDigit := unicode.IsDigit(next)

			if !prevSpace && (prevIsLower || prevIsDigit || prevIsDash ||
				(prevIsUpper && (nextIsLower || nextIsDigit))) {
				b.WriteRune(' ')
			}

			b.WriteRune(unicode.ToLower(r))

			prevSpace = false
		default:
			b.WriteRune(unicode.ToLower(r))

			prevSpace = false
		}
	}

	return strings.Join(strings.Fields(b.String()), "-")
}
