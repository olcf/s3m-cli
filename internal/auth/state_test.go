package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadState(t *testing.T) {
	t.Setenv("S3M_AUTH_PATH", filepath.Join(t.TempDir(), "auth.json"))

	st := NewState()
	rec := TokenRecord{
		Token:        "tok",
		Project:      "proj",
		Enclave:      "enc",
		Scopes:       []string{"scope1"},
		ExpiresAt:    new(time.Now()),
		OwnerName:    "owner",
		Username:     "user",
		OneTimeToken: true,
	}
	if err := st.PutToken(rec); err != nil {
		t.Fatalf("PutToken: %v", err)
	}
	if err := SaveState(os.Getenv("S3M_AUTH_PATH"), st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	info, err := os.Stat(os.Getenv("S3M_AUTH_PATH"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 perms, got %v", info.Mode().Perm())
	}

	loaded, err := LoadState(os.Getenv("S3M_AUTH_PATH"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	got, ok := loaded.CurrentToken()
	if !ok {
		t.Fatal("CurrentToken missing after load")
	}
	if got.Project != rec.Project || got.Enclave != rec.Enclave || got.Token != rec.Token {
		t.Fatalf("loaded token mismatch: %+v", got)
	}
}

func TestSwitchContext(t *testing.T) {
	st := NewState()
	first := TokenRecord{Token: "a", Project: "p1", Enclave: "e1"}
	second := TokenRecord{Token: "b", Project: "p2", Enclave: "e2"}
	if err := st.PutToken(first); err != nil {
		t.Fatalf("PutToken first: %v", err)
	}
	if err := st.PutToken(second); err != nil {
		t.Fatalf("PutToken second: %v", err)
	}

	if _, err := st.SwitchContext("e1", "p1"); err != nil {
		t.Fatalf("SwitchContext existing: %v", err)
	}

	got, ok := st.CurrentToken()
	if !ok {
		t.Fatal("expected current token after switch")
	}
	if got.Token != first.Token || got.Enclave != first.Enclave || got.Project != first.Project {
		t.Fatalf("unexpected current token after switch: %+v", got)
	}
	if st.Current != (Context{Enclave: "e1", Project: "p1"}) {
		t.Fatalf("unexpected current context after switch: %+v", st.Current)
	}

	if _, err := st.SwitchContext("missing", "proj"); err == nil {
		t.Fatal("expected error for missing context")
	}
}
