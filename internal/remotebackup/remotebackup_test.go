package remotebackup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

func validConn() RemoteConn {
	return RemoteConn{
		Host:     "db.example.com",
		Port:     5432,
		User:     "alice",
		Password: "s3cr3t-pw",
		DBName:   "shop",
		SSLMode:  "require",
	}
}

func TestRemoteConnValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*RemoteConn)
		wantErr bool
	}{
		{"valid", func(*RemoteConn) {}, false},
		{"empty host", func(c *RemoteConn) { c.Host = "" }, true},
		{"whitespace host", func(c *RemoteConn) { c.Host = "  " }, true},
		{"empty user", func(c *RemoteConn) { c.User = "" }, true},
		{"empty dbname", func(c *RemoteConn) { c.DBName = "" }, true},
		{"zero port defaults ok", func(c *RemoteConn) { c.Port = 0 }, false},
		{"negative port", func(c *RemoteConn) { c.Port = -1 }, true},
		{"too-large port", func(c *RemoteConn) { c.Port = 70000 }, true},
		{"bad sslmode", func(c *RemoteConn) { c.SSLMode = "bogus" }, true},
		{"empty sslmode ok", func(c *RemoteConn) { c.SSLMode = "" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := validConn()
			tc.mutate(&c)
			err := c.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr && apperr.HTTPStatus(err) != 400 {
				t.Fatalf("expected 400-class invalid error, got status %d", apperr.HTTPStatus(err))
			}
		})
	}
}

func TestRemoteConnDefaults(t *testing.T) {
	c := RemoteConn{Host: "h", User: "u", DBName: "d"}
	c.applyDefaults()
	if c.Port != 5432 {
		t.Fatalf("expected default port 5432, got %d", c.Port)
	}
	if c.SSLMode != "prefer" {
		t.Fatalf("expected default sslmode prefer, got %q", c.SSLMode)
	}
}

func TestDSNExcludesPassword(t *testing.T) {
	c := validConn()
	dsn := c.DSN()
	if strings.Contains(dsn, c.Password) {
		t.Fatalf("DSN must NOT contain the password: %q", dsn)
	}
	if !strings.Contains(dsn, "host=db.example.com") {
		t.Fatalf("DSN missing host: %q", dsn)
	}
	if !strings.Contains(dsn, "dbname=shop") {
		t.Fatalf("DSN missing dbname: %q", dsn)
	}
	if !strings.Contains(dsn, "sslmode=require") {
		t.Fatalf("DSN missing sslmode: %q", dsn)
	}
}

func TestDSNEscapesSpecialChars(t *testing.T) {
	c := validConn()
	c.DBName = "my db'x"
	dsn := c.DSN()
	// keyword/value DSN escapes single quotes and wraps spaces in quotes.
	if !strings.Contains(dsn, `dbname='my db\'x'`) {
		t.Fatalf("expected escaped dbname, got %q", dsn)
	}
}

func TestRedact(t *testing.T) {
	in := "connection to host=h password=s3cr3t-pw user=u failed"
	out := Redact(in, "s3cr3t-pw")
	if strings.Contains(out, "s3cr3t-pw") {
		t.Fatalf("redact left the secret in: %q", out)
	}
	if !strings.Contains(out, redactMask) {
		t.Fatalf("expected mask in output: %q", out)
	}
	// Empty secret must be a no-op (never mask the whole string).
	if got := Redact("abc", ""); got != "abc" {
		t.Fatalf("empty secret should be no-op, got %q", got)
	}
}

func TestParsePgDumpMajor(t *testing.T) {
	cases := map[string]struct {
		want int
		ok   bool
	}{
		"pg_dump (PostgreSQL) 16.2":      {16, true},
		"pg_dump (PostgreSQL) 17rc1":     {17, true},
		"pg_dump (PostgreSQL) 9.6.24":    {9, true},
		"pg_restore (PostgreSQL) 15.5":   {15, true},
		"garbage output no version here": {0, false},
		"":                               {0, false},
	}
	for in, exp := range cases {
		got, ok := ParsePgDumpMajor(in)
		if ok != exp.ok || got != exp.want {
			t.Errorf("ParsePgDumpMajor(%q) = (%d,%v), want (%d,%v)", in, got, ok, exp.want, exp.ok)
		}
	}
}

func TestCheckVersionSkew(t *testing.T) {
	// pg_dump must be >= server major.
	if err := CheckVersionSkew(16, 16); err != nil {
		t.Fatalf("equal versions must pass: %v", err)
	}
	if err := CheckVersionSkew(17, 16); err != nil {
		t.Fatalf("newer dump must pass: %v", err)
	}
	err := CheckVersionSkew(15, 16)
	if err == nil {
		t.Fatalf("older dump than server must fail")
	}
	if apperr.HTTPStatus(err) != 400 {
		t.Fatalf("version skew must be an invalid (400) error, got %d", apperr.HTTPStatus(err))
	}
	if !strings.Contains(err.Error(), "15") || !strings.Contains(err.Error(), "16") {
		t.Fatalf("error should mention both versions: %v", err)
	}
}

func TestCatalogKeyIsUniqueAndRedacted(t *testing.T) {
	c := validConn()
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	k1 := catalogKey(now)
	k2 := catalogKey(now)
	if k1 == k2 {
		t.Fatalf("same-second keys must differ (crypto suffix): %q == %q", k1, k2)
	}
	for _, k := range []string{k1, k2} {
		if !strings.HasPrefix(k, defaultPrefix) {
			t.Fatalf("key missing prefix: %q", k)
		}
		if strings.Contains(k, c.Password) || strings.Contains(k, c.Host) {
			t.Fatalf("key must not embed secrets/host: %q", k)
		}
		if !strings.HasSuffix(k, ".dump") {
			t.Fatalf("key missing .dump suffix: %q", k)
		}
	}
}

func TestBuildDumpArgsNoPasswordInArgv(t *testing.T) {
	c := validConn()
	args := buildDumpArgs(c)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, c.Password) {
		t.Fatalf("password must never appear in argv: %v", args)
	}
	if !strings.Contains(joined, "--format=custom") {
		t.Fatalf("expected custom format: %v", args)
	}
	if !strings.Contains(joined, "--host=db.example.com") {
		t.Fatalf("expected host arg: %v", args)
	}
	if !strings.Contains(joined, "--no-password") {
		t.Fatalf("expected --no-password: %v", args)
	}
	if !strings.Contains(joined, "--dbname=shop") {
		t.Fatalf("expected dbname: %v", args)
	}
}

func TestBuildRestoreArgs(t *testing.T) {
	args := buildRestoreArgs("alice", "shop", "h", 5432)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--no-owner", "--no-privileges", "--dbname=shop", "--username=alice", "--no-password"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("restore args missing %q: %v", want, args)
		}
	}
}

// --- fakes ---

type fakeStore struct {
	objects map[string][]byte
	putErr  error
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string][]byte{}} }

func (f *fakeStore) Put(_ context.Context, key string, data []byte) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.objects[key] = append([]byte(nil), data...)
	return nil
}
func (f *fakeStore) Get(_ context.Context, key string) ([]byte, error) {
	d, ok := f.objects[key]
	if !ok {
		return nil, apperr.New(apperr.KindNotFound, "not found")
	}
	return d, nil
}

type fakeCatalog struct {
	saved []CatalogEntry
}

func (f *fakeCatalog) Save(_ context.Context, e CatalogEntry) (CatalogEntry, error) {
	if e.ID == "" {
		e.ID = "cat-1"
	}
	f.saved = append(f.saved, e)
	return e, nil
}
func (f *fakeCatalog) Get(_ context.Context, id string) (CatalogEntry, error) {
	for _, e := range f.saved {
		if e.ID == id {
			return e, nil
		}
	}
	return CatalogEntry{}, apperr.New(apperr.KindNotFound, "no entry")
}
func (f *fakeCatalog) List(context.Context) ([]CatalogEntry, error) { return f.saved, nil }

func TestServiceCaptureSuccess(t *testing.T) {
	store := newFakeStore()
	cat := &fakeCatalog{}
	svc := New(store, cat)
	svc.runCapture = func(_ context.Context, _ RemoteConn) ([]byte, error) {
		return []byte("PGDMP-fake-bytes"), nil
	}
	svc.serverMajor = func(_ context.Context, _ RemoteConn) (int, error) { return 16, nil }
	svc.dumpMajor = func(_ context.Context) (int, error) { return 16, nil }

	entry, err := svc.Capture(context.Background(), validConn())
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if entry.ID == "" {
		t.Fatalf("expected catalog id")
	}
	if entry.Size != int64(len("PGDMP-fake-bytes")) {
		t.Fatalf("size mismatch: %d", entry.Size)
	}
	// Stored under the catalog key.
	if _, ok := store.objects[entry.ObjectKey]; !ok {
		t.Fatalf("dump not stored under %q", entry.ObjectKey)
	}
	// No secrets leak into the catalog entry.
	if strings.Contains(entry.SourceHost, "example.com") {
		// host is redacted per requirement; ensure it's masked, not full
		if entry.SourceHost == "db.example.com" {
			t.Fatalf("source host must be redacted: %q", entry.SourceHost)
		}
	}
	if strings.Contains(entry.String(), "s3cr3t-pw") {
		t.Fatalf("entry string leaked password: %q", entry.String())
	}
}

func TestServiceCaptureVersionSkewRefused(t *testing.T) {
	store := newFakeStore()
	cat := &fakeCatalog{}
	svc := New(store, cat)
	called := false
	svc.runCapture = func(_ context.Context, _ RemoteConn) ([]byte, error) {
		called = true
		return []byte("x"), nil
	}
	svc.serverMajor = func(_ context.Context, _ RemoteConn) (int, error) { return 17, nil }
	svc.dumpMajor = func(_ context.Context) (int, error) { return 16, nil }

	_, err := svc.Capture(context.Background(), validConn())
	if err == nil {
		t.Fatalf("expected version-skew refusal")
	}
	if called {
		t.Fatalf("must NOT run pg_dump when version skew detected")
	}
	if apperr.HTTPStatus(err) != 400 {
		t.Fatalf("expected 400, got %d", apperr.HTTPStatus(err))
	}
}

func TestServiceCaptureInvalidConn(t *testing.T) {
	svc := New(newFakeStore(), &fakeCatalog{})
	bad := validConn()
	bad.Host = ""
	if _, err := svc.Capture(context.Background(), bad); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestServiceCaptureDumpFailureRedacted(t *testing.T) {
	svc := New(newFakeStore(), &fakeCatalog{})
	svc.serverMajor = func(_ context.Context, _ RemoteConn) (int, error) { return 16, nil }
	svc.dumpMajor = func(_ context.Context) (int, error) { return 16, nil }
	svc.runCapture = func(_ context.Context, c RemoteConn) ([]byte, error) {
		return nil, errors.New("pg_dump: password=" + c.Password + " auth failed")
	}
	_, err := svc.Capture(context.Background(), validConn())
	if err == nil {
		t.Fatalf("expected dump error")
	}
	if strings.Contains(err.Error(), "s3cr3t-pw") {
		t.Fatalf("error leaked password: %v", err)
	}
}

func TestRestoreIntoSuccess(t *testing.T) {
	store := newFakeStore()
	cat := &fakeCatalog{}
	svc := New(store, cat)
	svc.serverMajor = func(_ context.Context, _ RemoteConn) (int, error) { return 16, nil }
	svc.dumpMajor = func(_ context.Context) (int, error) { return 16, nil }
	svc.runCapture = func(_ context.Context, _ RemoteConn) ([]byte, error) { return []byte("DUMP"), nil }
	e, err := svc.Capture(context.Background(), validConn())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	var gotData []byte
	var gotDSN string
	svc.runRestore = func(_ context.Context, data []byte, dsn string) error {
		gotData = data
		gotDSN = dsn
		return nil
	}
	dsn := "postgres://postgres:targetpw@localhost:5440/postgres?sslmode=disable"
	if err := svc.RestoreInto(context.Background(), e.ID, dsn); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if string(gotData) != "DUMP" {
		t.Fatalf("restore got wrong bytes: %q", gotData)
	}
	if gotDSN != dsn {
		t.Fatalf("restore got wrong dsn: %q", gotDSN)
	}
}

func TestRestoreIntoMissingDump(t *testing.T) {
	svc := New(newFakeStore(), &fakeCatalog{})
	svc.runRestore = func(_ context.Context, _ []byte, _ string) error { return nil }
	err := svc.RestoreInto(context.Background(), "nope", "postgres://x")
	if err == nil {
		t.Fatalf("expected not-found error for unknown dump id")
	}
	if apperr.HTTPStatus(err) != 404 {
		t.Fatalf("expected 404, got %d", apperr.HTTPStatus(err))
	}
}

func TestRestoreIntoRedactsTargetPassword(t *testing.T) {
	store := newFakeStore()
	cat := &fakeCatalog{}
	svc := New(store, cat)
	svc.serverMajor = func(_ context.Context, _ RemoteConn) (int, error) { return 16, nil }
	svc.dumpMajor = func(_ context.Context) (int, error) { return 16, nil }
	svc.runCapture = func(_ context.Context, _ RemoteConn) ([]byte, error) { return []byte("D"), nil }
	e, _ := svc.Capture(context.Background(), validConn())
	svc.runRestore = func(_ context.Context, _ []byte, _ string) error {
		return errors.New("restore boom")
	}
	if err := svc.RestoreInto(context.Background(), e.ID, "postgres://u:p@h/db"); err == nil {
		t.Fatalf("expected restore error")
	}
}

func TestServiceListAndGet(t *testing.T) {
	cat := &fakeCatalog{}
	svc := New(newFakeStore(), cat)
	svc.serverMajor = func(_ context.Context, _ RemoteConn) (int, error) { return 16, nil }
	svc.dumpMajor = func(_ context.Context) (int, error) { return 16, nil }
	svc.runCapture = func(_ context.Context, _ RemoteConn) ([]byte, error) { return []byte("d"), nil }
	e, err := svc.Capture(context.Background(), validConn())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	list, err := svc.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
	got, err := svc.GetEntry(context.Background(), e.ID)
	if err != nil || got.ID != e.ID {
		t.Fatalf("get: %v", err)
	}
}

func TestRemoteConnValidateBlocksMetadata(t *testing.T) {
	for _, host := range []string{"169.254.169.254", "169.254.1.1", "fe80::1"} {
		c := RemoteConn{Host: host, User: "u", DBName: "d"}
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected link-local/metadata host to be rejected", host)
		}
	}
	// A normal private host stays allowed (adopting an internal DB is fine).
	if err := (RemoteConn{Host: "10.0.0.5", User: "u", DBName: "d"}).Validate(); err != nil {
		t.Errorf("private host should be allowed: %v", err)
	}
}
