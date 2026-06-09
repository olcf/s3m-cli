package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Context struct {
	Enclave string `json:"enclave"`
	Project string `json:"project"`
}

type TokenRecord struct {
	Token                   string     `json:"token"`
	Project                 string     `json:"project"`
	Enclave                 string     `json:"enclave"`
	Username                string     `json:"username,omitempty"`
	OwnerName               string     `json:"ownerName,omitempty"`
	Description             string     `json:"description,omitempty"`
	Scopes                  []string   `json:"scopes,omitempty"`
	Permissions             []string   `json:"permissions,omitempty"`
	ExpiresAt               *time.Time `json:"expiresAt,omitempty"`
	IntrospectedAt          *time.Time `json:"introspectedAt,omitempty"`
	LastIntrospectionError  string     `json:"lastIntrospectionError,omitempty"`
	OneTimeToken            bool       `json:"oneTimeToken,omitempty"`
	DelayedStart            bool       `json:"delayedStart,omitempty"`
	DelayedStartDate        *time.Time `json:"delayedStartDate,omitempty"`
	PlannedExpirationSource string     `json:"plannedExpirationSource,omitempty"`
}

type State struct {
	Current Context                `json:"current"`
	Tokens  map[string]TokenRecord `json:"tokens"`
}

func NewState() *State {
	return &State{
		Tokens: make(map[string]TokenRecord),
	}
}

func DefaultStatePath() (string, error) {
	if override := os.Getenv("S3M_AUTH_PATH"); override != "" {
		return override, nil
	}

	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("detect home directory: %w", err)
		}

		switch runtime.GOOS {
		case "darwin":
			base = filepath.Join(home, "Library", "Application Support")
		case "windows":
			base = filepath.Join(home, "AppData", "Local")
		default:
			base = filepath.Join(home, ".local", "share")
		}
	}

	return filepath.Join(base, "s3m", "auth.json"), nil
}

func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path can be overridden by S3M_AUTH_PATH env var
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewState(), nil
		}

		return nil, fmt.Errorf("read state: %w", err)
	}

	var st State

	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}

	if st.Tokens == nil {
		st.Tokens = make(map[string]TokenRecord)
	}

	return &st, nil
}

func SaveState(path string, st *State) error {
	if st == nil {
		return errors.New("nil state")
	}

	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmp := path + ".tmp"
	renamed := false

	defer func() {
		if renamed {
			return
		}

		if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Debug("failed to remove temp state file", "path", tmp, "error", err)
		}
	}()

	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}

	renamed = true

	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set state permissions: %w", err)
	}

	return nil
}

func (s *State) CurrentToken() (TokenRecord, bool) {
	if s == nil {
		return TokenRecord{}, false
	}

	if s.Current.Project == "" || s.Current.Enclave == "" {
		return TokenRecord{}, false
	}

	rec, ok := s.Tokens[keyFor(s.Current.Enclave, s.Current.Project)]

	return rec, ok
}

func (s *State) PutToken(rec TokenRecord) error {
	if s == nil {
		return errors.New("state is nil")
	}

	rec.Project = strings.TrimSpace(rec.Project)
	rec.Enclave = strings.TrimSpace(rec.Enclave)

	if rec.Project == "" || rec.Enclave == "" {
		return errors.New("project and enclave are required to store a token")
	}

	k := keyFor(rec.Enclave, rec.Project)
	s.Tokens[k] = rec
	s.Current = Context{Enclave: rec.Enclave, Project: rec.Project}

	return nil
}

func (s *State) SwitchContext(enclave, project string) (TokenRecord, error) {
	if s == nil {
		return TokenRecord{}, errors.New("state is nil")
	}

	k := keyFor(enclave, project)
	rec, ok := s.Tokens[k]

	if !ok {
		return TokenRecord{}, fmt.Errorf("no token stored for enclave %q and project %q", enclave, project)
	}

	s.Current = Context{Enclave: rec.Enclave, Project: rec.Project}

	return rec, nil
}

func (s *State) ListContexts() []Context {
	if s == nil {
		return nil
	}

	out := make([]Context, 0, len(s.Tokens))

	for _, rec := range s.Tokens {
		out = append(out, Context{Enclave: rec.Enclave, Project: rec.Project})
	}

	return out
}

func (tr TokenRecord) IntrospectionOK() bool {
	return tr.LastIntrospectionError == ""
}

func keyFor(enclave, project string) string {
	return strings.ToLower(strings.TrimSpace(enclave)) + "|" + strings.ToLower(strings.TrimSpace(project))
}
