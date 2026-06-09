package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tmspb "github.com/olcf/s3m-apis/tms/v1"

	grpcclient "github.com/olcf/s3m-cli/internal/grpc"
	"github.com/olcf/s3m-cli/internal/util"
)

type IntrospectionResult struct {
	Record TokenRecord
	Err    error
}

func IntrospectToken(
	ctx context.Context, target, token string, connectTimeout, callTimeout time.Duration, debug bool,
) IntrospectionResult {
	res := IntrospectionResult{
		Record: TokenRecord{
			Token: token,
		},
	}

	conn, err := grpcclient.DialAndWait(ctx, target, token, connectTimeout, debug)
	if err != nil {
		res.Err = fmt.Errorf("dial token control: %w", err)
		return res
	}

	defer func() {
		if closeErr := conn.Close(); closeErr != nil && res.Err == nil {
			res.Err = fmt.Errorf("close connection: %w", closeErr)
		}
	}()

	client := tmspb.NewTokenControlClient(conn)

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	resp, err := client.IntrospectAuthToken(callCtx, &tmspb.IntrospectAuthTokenRequest{
		GrpcPermissions: true,
	})
	if err != nil {
		res.Err = fmt.Errorf("introspect token: %w", err)
		return res
	}

	td := resp.GetToken()
	if td == nil {
		res.Err = errors.New("introspection response missing token details")
		return res
	}

	res.Record.Project = td.GetProject()
	res.Record.Enclave = td.GetSecurityEnclave()
	res.Record.Username = td.GetUsername()
	res.Record.OwnerName = td.GetOwnerName()
	res.Record.Description = td.GetDescription()
	res.Record.Scopes = collectScopes(td)
	res.Record.Permissions = collectPermissions(td)
	res.Record.OneTimeToken = td.GetOneTimeToken()
	res.Record.DelayedStart = td.GetDelayedStart()
	res.Record.IntrospectedAt = new(time.Now().UTC())

	if td.GetDelayDate() != nil {
		res.Record.DelayedStartDate = new(td.GetDelayDate().AsTime())
	}

	if td.GetPlannedExpiration() != nil {
		res.Record.ExpiresAt = new(td.GetPlannedExpiration().AsTime())
		res.Record.PlannedExpirationSource = "introspected"
	}

	return res
}

func collectScopes(td *tmspb.TokenDetailsIntrospective) []string {
	if td == nil {
		return nil
	}

	var scopes []string

	scopes = append(scopes, td.GetPermissions()...)
	for _, gp := range td.GetGrpcPermissions() {
		path := strings.TrimSpace(gp.GetPath())
		if path != "" {
			scopes = append(scopes, strings.ToLower(path))
		}

		if perm := strings.TrimSpace(gp.GetPermission()); perm != "" {
			scopes = append(scopes, strings.ToLower(perm))
		}
	}

	return util.DedupeStrings(scopes)
}

func collectPermissions(td *tmspb.TokenDetailsIntrospective) []string {
	if td == nil {
		return nil
	}

	perms := make([]string, 0, len(td.GetPermissions())+len(td.GetGrpcPermissions())*2)
	perms = append(perms, td.GetPermissions()...)

	for _, gp := range td.GetGrpcPermissions() {
		if path := strings.TrimSpace(gp.GetPath()); path != "" {
			perms = append(perms, path)
		}

		if perm := strings.TrimSpace(gp.GetPermission()); perm != "" {
			perms = append(perms, perm)
		}
	}

	return util.DedupeStrings(perms)
}
