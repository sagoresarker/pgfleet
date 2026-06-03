package remotebackup

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// ObjectStore is the subset of the object store the service needs. It is
// satisfied by a thin adapter over internal/objectstore (see objectStoreAdapter
// below).
type ObjectStore interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
}

// CatalogEntry is a persisted record of one captured remote dump. It carries a
// REDACTED source host and never any password.
type CatalogEntry struct {
	ID         string
	ObjectKey  string
	SourceHost string // redacted (e.g. "[REDACTED].com")
	SourceDB   string
	ServerMaj  int
	Size       int64
	CreatedAt  time.Time
}

// String returns a safe, password-free description for logs.
func (e CatalogEntry) String() string {
	return "remote-dump id=" + e.ID + " key=" + e.ObjectKey +
		" host=" + e.SourceHost + " db=" + e.SourceDB +
		" server_major=" + strconv.Itoa(e.ServerMaj) +
		" size=" + strconv.FormatInt(e.Size, 10)
}

// Catalog persists and reads CatalogEntry records.
type Catalog interface {
	Save(ctx context.Context, e CatalogEntry) (CatalogEntry, error)
	Get(ctx context.Context, id string) (CatalogEntry, error)
	List(ctx context.Context) ([]CatalogEntry, error)
}

// Service captures remote dumps into the object store and catalogs them. The
// exec-ish collaborators (runCapture, serverMajor, dumpMajor) are fields so
// tests can substitute fakes without a real database or pg_dump on PATH.
type Service struct {
	store   ObjectStore
	catalog Catalog
	now     func() time.Time

	// runCapture runs pg_dump against the remote and returns the dump bytes.
	runCapture func(ctx context.Context, c RemoteConn) ([]byte, error)
	// serverMajor reports the remote server's major version.
	serverMajor func(ctx context.Context, c RemoteConn) (int, error)
	// dumpMajor reports the local pg_dump major version.
	dumpMajor func(ctx context.Context) (int, error)
	// runRestore pipes dump bytes into pg_restore against a target DSN.
	// nil means use realRestore (see runRestoreFn).
	runRestore func(ctx context.Context, data []byte, targetDSN string) error
}

// New builds a Service wired to real pg_dump/psql collaborators. Tests override
// the runCapture/serverMajor/dumpMajor fields.
func New(store ObjectStore, catalog Catalog) *Service {
	s := &Service{
		store:   store,
		catalog: catalog,
		now:     time.Now,
	}
	s.runCapture = realCapture
	s.serverMajor = realServerMajor
	s.dumpMajor = realDumpMajor
	return s
}

// Capture validates the connection, checks version-skew safety, runs a
// custom-format pg_dump against the remote, stores the dump under a unique key,
// and catalogs it. Every error is redacted of the password.
func (s *Service) Capture(ctx context.Context, c RemoteConn) (CatalogEntry, error) {
	if err := c.Validate(); err != nil {
		return CatalogEntry{}, err
	}
	c.applyDefaults()

	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()

	srvMaj, err := s.serverMajor(ctx, c)
	if err != nil {
		return CatalogEntry{}, s.redactErr(c, "remotebackup: probe remote server version", err)
	}
	dMaj, err := s.dumpMajor(ctx)
	if err != nil {
		return CatalogEntry{}, s.redactErr(c, "remotebackup: probe pg_dump version", err)
	}
	if err := CheckVersionSkew(dMaj, srvMaj); err != nil {
		return CatalogEntry{}, err
	}

	data, err := s.runCapture(ctx, c)
	if err != nil {
		return CatalogEntry{}, s.redactErr(c, "remotebackup: pg_dump failed", err)
	}
	if len(data) == 0 {
		return CatalogEntry{}, apperr.New(apperr.KindInternal, "remotebackup: pg_dump produced no output")
	}

	key := catalogKey(s.now())
	if err := s.store.Put(ctx, key, data); err != nil {
		return CatalogEntry{}, s.redactErr(c, "remotebackup: store dump", err)
	}

	entry := CatalogEntry{
		ObjectKey:  key,
		SourceHost: redactHost(c.Host),
		SourceDB:   c.DBName,
		ServerMaj:  srvMaj,
		Size:       int64(len(data)),
		CreatedAt:  s.now().UTC(),
	}
	saved, err := s.catalog.Save(ctx, entry)
	if err != nil {
		return CatalogEntry{}, s.redactErr(c, "remotebackup: catalog dump", err)
	}
	return saved, nil
}

// List returns all cataloged remote dumps.
func (s *Service) List(ctx context.Context) ([]CatalogEntry, error) {
	return s.catalog.List(ctx)
}

// GetEntry returns one cataloged remote dump by id.
func (s *Service) GetEntry(ctx context.Context, id string) (CatalogEntry, error) {
	return s.catalog.Get(ctx, id)
}

// Fetch returns the raw dump bytes for a cataloged entry (used by the restore
// orchestration).
func (s *Service) Fetch(ctx context.Context, id string) (CatalogEntry, []byte, error) {
	e, err := s.catalog.Get(ctx, id)
	if err != nil {
		return CatalogEntry{}, nil, err
	}
	data, err := s.store.Get(ctx, e.ObjectKey)
	if err != nil {
		return CatalogEntry{}, nil, err
	}
	return e, data, nil
}

// redactErr wraps cause with msg, stripping the remote password from both the
// message and the cause text. The cause's classification (e.g. a store
// NotFound) is preserved so callers map it to the right HTTP status, but its
// text is re-wrapped so no secret survives into the surfaced error.
func (s *Service) redactErr(c RemoteConn, msg string, cause error) error {
	safeMsg := Redact(msg, c.Password)
	if cause == nil {
		return apperr.New(apperr.KindInternal, safeMsg)
	}
	safeCause := apperr.New(apperr.Kind(cause), Redact(cause.Error(), c.Password))
	return apperr.Wrap(apperr.Kind(cause), safeMsg, safeCause)
}

// --- real (production) collaborators ---

// realServerMajor connects to the remote and reads server_version_num. The
// password is passed via PGPASSWORD (never argv).
func realServerMajor(ctx context.Context, c RemoteConn) (int, error) {
	cmd := exec.CommandContext(ctx, "psql",
		"--host="+c.Host, "--port="+strconv.Itoa(c.Port),
		"--username="+c.User, "--dbname="+c.DBName, "--no-password",
		"-tAX", "-c", "SHOW server_version_num")
	cmd.Env = withPGPassword(c.Password)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, apperr.New(apperr.KindInternal,
			"psql server_version probe: "+strings.TrimSpace(stderr.String()))
	}
	num, perr := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if perr != nil || num <= 0 {
		return 0, apperr.New(apperr.KindInternal, "could not parse server_version_num")
	}
	return serverMajorFromVersionNum(num), nil
}

// realDumpMajor reads the local pg_dump major version.
func realDumpMajor(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "pg_dump", "--version").Output()
	if err != nil {
		return 0, apperr.Wrap(apperr.KindInternal, "pg_dump --version (is it installed?)", err)
	}
	maj, ok := ParsePgDumpMajor(string(out))
	if !ok {
		return 0, apperr.New(apperr.KindInternal, "could not parse pg_dump version")
	}
	return maj, nil
}

// realCapture runs `pg_dump --format=custom` against the remote and returns the
// captured bytes. The password is supplied via PGPASSWORD.
func realCapture(ctx context.Context, c RemoteConn) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "pg_dump", buildDumpArgs(c)...)
	cmd.Env = withPGPassword(c.Password)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, apperr.New(apperr.KindInternal,
			"pg_dump: "+strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// withPGPassword returns the parent environment plus PGPASSWORD (only when a
// password is set), so the secret never appears in argv / `ps`.
func withPGPassword(password string) []string {
	env := os.Environ()
	if password != "" {
		env = append(env, "PGPASSWORD="+password)
	}
	return env
}
