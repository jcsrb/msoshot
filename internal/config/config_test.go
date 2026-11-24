package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesWithToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "127.0.0.1" || c.Port != DefaultPort || len(c.Token) < 40 {
		t.Fatalf("defaults: %+v", c)
	}
	again, err := Load(path)
	if err != nil || again.Token != c.Token {
		t.Fatal("token not persisted")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("config permissions %v", info.Mode().Perm())
	}
}

func TestValidate(t *testing.T) {
	good := Config{Host: "100.101.102.103", Port: 8787, Token: NewToken()}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Config{
		{Host: "localhost", Port: 1, Token: good.Token},
		{Host: "::1", Port: 1, Token: good.Token},
		{Host: "127.0.0.1", Port: 0, Token: good.Token},
		{Host: "127.0.0.1", Port: 1, Token: "short"},
	} {
		if bad.Validate() == nil {
			t.Errorf("expected invalid: %+v", bad)
		}
	}
}

func TestLocalAddresses(t *testing.T) {
	a := LocalAddresses()
	if len(a) == 0 || a[0].IP != "127.0.0.1" {
		t.Fatalf("%+v", a)
	}
}

func TestMigrate(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old", "config.json")
	path := filepath.Join(dir, "new", "config.json")
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte(`{"token":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	migrate(old, path)
	if data, _ := os.ReadFile(path); string(data) != `{"token":"x"}` {
		t.Fatalf("not migrated: %q", data)
	}
	// An existing new config is never overwritten.
	_ = os.WriteFile(old, []byte(`{"token":"y"}`), 0o600)
	migrate(old, path)
	if data, _ := os.ReadFile(path); string(data) != `{"token":"x"}` {
		t.Fatalf("overwritten: %q", data)
	}
}
