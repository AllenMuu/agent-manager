package memorypipeline

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const schemaVersion = 3

var (
	ErrFTS5Unavailable          = errors.New("SQLite FTS5 is unavailable")
	ErrUnsupportedSchemaVersion = errors.New("unsupported memory pipeline schema version")
	ErrPipelineNotInitialized   = errors.New("project memory pipeline is not initialized")
	ErrReadOnlyStore            = errors.New("memory pipeline store is read-only")
)

// Store owns only derived memory. The canonical Memory provider and run store
// remain outside this database.
type Store struct {
	db       *sql.DB
	path     string
	fts5     bool
	readOnly bool
}

// InitializeProject creates the project-local pipeline database only when the
// operator explicitly initializes the feature.
func InitializeProject(projectRoot string) (*Store, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, errors.New("project root is required")
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect project root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("project root must be a real directory")
	}
	stateDir := filepath.Join(root, ".agent-manager")
	info, err = os.Lstat(stateDir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			return nil, fmt.Errorf("create project memory directory: %w", err)
		}
		info, err = os.Lstat(stateDir)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect project memory directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("project .agent-manager path must be a real directory")
	}
	return OpenStore(filepath.Join(stateDir, "memory.db"))
}

// OpenProjectStore opens an existing project database without creating
// directories or files. Only InitializeProject creates pipeline state.
func OpenProjectStore(projectRoot string) (*Store, error) {
	path, err := projectStorePath(projectRoot)
	if err != nil {
		return nil, err
	}
	return OpenStore(path)
}

// OpenProjectStoreReadOnly opens an existing project database without schema
// or data mutations. It never initializes an absent database.
func OpenProjectStoreReadOnly(projectRoot string) (*Store, error) {
	path, err := projectStorePath(projectRoot)
	if err != nil {
		return nil, err
	}
	return OpenReadOnlyStore(path)
}

func projectStorePath(projectRoot string) (string, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return "", errors.New("project root is required")
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrPipelineNotInitialized
	}
	if err != nil {
		return "", fmt.Errorf("inspect project root: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("project root must be a real directory")
	}
	stateDir := filepath.Join(root, ".agent-manager")
	info, err := os.Lstat(stateDir)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrPipelineNotInitialized
	}
	if err != nil {
		return "", fmt.Errorf("inspect project memory directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("project .agent-manager path must be a real directory")
	}
	path := filepath.Join(stateDir, "memory.db")
	if err := rejectSymlinkOrSpecial(path); err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return "", ErrPipelineNotInitialized
	} else if err != nil {
		return "", fmt.Errorf("inspect project memory database: %w", err)
	}
	return path, nil
}

// OpenReadOnlyStore opens an existing supported database without running
// schema setup or changing SQLite journal settings. SQLite may initialize
// read-only WAL sidecars when opening a WAL-mode database.
func OpenReadOnlyStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("memory pipeline database path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve memory pipeline path: %w", err)
	}
	parent, err := os.Lstat(filepath.Dir(abs))
	if err != nil {
		return nil, fmt.Errorf("inspect memory pipeline directory: %w", err)
	}
	if !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("memory pipeline parent must be a real directory")
	}
	if err := rejectSymlinkOrSpecial(abs); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := rejectSymlinkOrSpecial(abs + suffix); err != nil {
			return nil, err
		}
	}
	dsn := (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open read-only memory pipeline database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return fail(fmt.Errorf("open read-only memory pipeline database: %w", err))
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(fmt.Errorf("read memory pipeline schema version: %w", err))
	}
	if version < 1 || version > schemaVersion {
		return fail(fmt.Errorf("%w: database has version %d, expected %d for read-only access", ErrUnsupportedSchemaVersion, version, schemaVersion))
	}
	return &Store{db: db, path: abs, readOnly: true}, nil
}

// OpenStore opens or creates a database at path. It rejects symlinked database
// and parent paths and does not replace data with an unknown schema version.
func OpenStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("memory pipeline database path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve memory pipeline path: %w", err)
	}
	parent, err := os.Lstat(filepath.Dir(abs))
	if err != nil {
		return nil, fmt.Errorf("inspect memory pipeline directory: %w", err)
	}
	if !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("memory pipeline parent must be a real directory")
	}
	if err := rejectSymlinkOrSpecial(abs); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := rejectSymlinkOrSpecial(abs + suffix); err != nil {
			return nil, err
		}
	}
	dsn := (&url.URL{Scheme: "file", Path: abs}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open memory pipeline database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, path: abs}
	fail := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return fail(fmt.Errorf("open memory pipeline database: %w", err))
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(fmt.Errorf("read memory pipeline schema version: %w", err))
	}
	if version > schemaVersion || version < 0 {
		return fail(fmt.Errorf("%w: database has version %d, supported version is %d", ErrUnsupportedSchemaVersion, version, schemaVersion))
	}
	if version == 0 {
		if err := ensureEmptyDatabase(db); err != nil {
			return fail(err)
		}
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return fail(fmt.Errorf("configure SQLite busy timeout: %w", err))
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fail(fmt.Errorf("configure SQLite foreign keys: %w", err))
	}
	if err := probeFTS5(db); err != nil {
		return fail(err)
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return fail(fmt.Errorf("configure SQLite journal mode: %w", err))
	}
	store.fts5 = true
	if version == 0 {
		if err := createSchema(db); err != nil {
			return fail(fmt.Errorf("initialize memory pipeline schema: %w", err))
		}
	} else if version < schemaVersion {
		if err := migrateSchema(db, version); err != nil {
			return fail(fmt.Errorf("migrate memory pipeline schema: %w", err))
		}
	}
	if err := secureSidecars(abs); err != nil {
		return fail(err)
	}
	return store, nil
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) FTS5Available() bool { return s != nil && s.fts5 }

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func rejectSymlinkOrSpecial(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect memory pipeline file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("memory pipeline database and sidecars must be regular files")
	}
	return nil
}

func probeFTS5(db *sql.DB) error {
	if _, err := db.Exec("CREATE VIRTUAL TABLE temp.__memory_pipeline_fts5_probe USING fts5(content)"); err != nil {
		return fmt.Errorf("%w: %v", ErrFTS5Unavailable, err)
	}
	if _, err := db.Exec("DROP TABLE temp.__memory_pipeline_fts5_probe"); err != nil {
		return fmt.Errorf("remove SQLite FTS5 probe: %w", err)
	}
	return nil
}

func ensureEmptyDatabase(db *sql.DB) error {
	var tables int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return fmt.Errorf("inspect unversioned SQLite database: %w", err)
	}
	if tables != 0 {
		return fmt.Errorf("%w: unversioned database already contains tables", ErrUnsupportedSchemaVersion)
	}
	return nil
}

func createSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE projects (
			project_id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			revision INTEGER NOT NULL DEFAULT 0,
			evidence_generation INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE evidence_refs (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			runtime TEXT NOT NULL,
			native_run_id TEXT NOT NULL,
			source_ref TEXT NOT NULL,
			created_at TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			import_status TEXT NOT NULL,
			imported_at TEXT NOT NULL,
			UNIQUE(project_id, runtime, native_run_id, content_hash),
			FOREIGN KEY(project_id) REFERENCES projects(project_id)
		)`,
		`CREATE TABLE derived_memories (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			field TEXT NOT NULL,
			knowledge_type TEXT NOT NULL,
			content TEXT NOT NULL,
			confidence REAL NOT NULL,
			state TEXT NOT NULL,
			verification TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_used_at TEXT,
			usage_count INTEGER NOT NULL DEFAULT 0,
			supersedes_derived_id TEXT,
			UNIQUE(project_id, field, content),
			FOREIGN KEY(project_id) REFERENCES projects(project_id),
			FOREIGN KEY(supersedes_derived_id) REFERENCES derived_memories(id)
		)`,
		`CREATE TABLE derived_memory_evidence (
			derived_id TEXT NOT NULL,
			evidence_id TEXT NOT NULL,
			PRIMARY KEY(derived_id, evidence_id),
			FOREIGN KEY(derived_id) REFERENCES derived_memories(id) ON DELETE CASCADE,
			FOREIGN KEY(evidence_id) REFERENCES evidence_refs(id)
		)`,
		`CREATE TABLE derived_supersessions (
			old_id TEXT PRIMARY KEY,
			new_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(old_id) REFERENCES derived_memories(id),
			FOREIGN KEY(new_id) REFERENCES derived_memories(id),
			FOREIGN KEY(project_id) REFERENCES projects(project_id)
		)`,
		`CREATE TABLE evidence_summaries (
			evidence_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(project_id),
			FOREIGN KEY(evidence_id) REFERENCES evidence_refs(id)
		)`,
		`CREATE TABLE project_summaries (
			project_id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			generation INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(project_id)
		)`,
		`CREATE TABLE summary_evidence (
			project_id TEXT NOT NULL,
			evidence_id TEXT NOT NULL,
			PRIMARY KEY(project_id, evidence_id),
			FOREIGN KEY(project_id) REFERENCES project_summaries(project_id) ON DELETE CASCADE,
			FOREIGN KEY(evidence_id) REFERENCES evidence_refs(id)
		)`,
		`CREATE TABLE proposals (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			derived_id TEXT,
			knowledge_type TEXT NOT NULL,
			content TEXT NOT NULL,
			status TEXT NOT NULL,
			operation_id TEXT NOT NULL,
			canonical_record_id TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES projects(project_id),
			FOREIGN KEY(derived_id) REFERENCES derived_memories(id)
		)`,
		`CREATE TABLE proposal_evidence (
			proposal_id TEXT NOT NULL,
			evidence_id TEXT NOT NULL,
			PRIMARY KEY(proposal_id, evidence_id),
			FOREIGN KEY(proposal_id) REFERENCES proposals(id) ON DELETE CASCADE,
			FOREIGN KEY(evidence_id) REFERENCES evidence_refs(id)
		)`,
		`CREATE UNIQUE INDEX proposals_by_derived ON proposals(project_id, derived_id) WHERE derived_id IS NOT NULL`,
		`CREATE TABLE skill_candidates (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			procedure_key TEXT NOT NULL,
			content TEXT NOT NULL,
			confidence REAL NOT NULL,
			evidence_generation INTEGER NOT NULL DEFAULT 0,
			review_state TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(project_id, procedure_key),
			FOREIGN KEY(project_id) REFERENCES projects(project_id)
		)`,
		`CREATE TABLE skill_candidate_evidence (
			candidate_id TEXT NOT NULL,
			evidence_id TEXT NOT NULL,
			PRIMARY KEY(candidate_id, evidence_id),
			FOREIGN KEY(candidate_id) REFERENCES skill_candidates(id) ON DELETE CASCADE,
			FOREIGN KEY(evidence_id) REFERENCES evidence_refs(id)
		)`,
		`CREATE TABLE procedure_claims (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			evidence_id TEXT NOT NULL,
			procedure_key TEXT NOT NULL,
			content TEXT NOT NULL,
			outcome TEXT NOT NULL,
			generalizable INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(project_id, evidence_id, procedure_key, content, outcome, generalizable),
			FOREIGN KEY(project_id) REFERENCES projects(project_id),
			FOREIGN KEY(evidence_id) REFERENCES evidence_refs(id)
		)`,
		`CREATE VIRTUAL TABLE derived_memory_fts USING fts5(id UNINDEXED, project_id UNINDEXED, content, tokenize='unicode61')`,
		`CREATE TRIGGER derived_memories_ai AFTER INSERT ON derived_memories BEGIN
			INSERT INTO derived_memory_fts(rowid, id, project_id, content) VALUES(new.rowid, new.id, new.project_id, new.content);
		END`,
		`CREATE TRIGGER derived_memories_ad AFTER DELETE ON derived_memories BEGIN
			DELETE FROM derived_memory_fts WHERE rowid = old.rowid;
		END`,
		`CREATE TRIGGER derived_memories_au AFTER UPDATE OF id, project_id, content ON derived_memories BEGIN
			DELETE FROM derived_memory_fts WHERE rowid = old.rowid;
			INSERT INTO derived_memory_fts(rowid, id, project_id, content) VALUES(new.rowid, new.id, new.project_id, new.content);
		END`,
		"PRAGMA user_version = 3",
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func migrateSchema(db *sql.DB, from int) error {
	if from < 1 || from >= schemaVersion {
		return fmt.Errorf("%w: no migration path from version %d", ErrUnsupportedSchemaVersion, from)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	if from == 1 {
		if _, err := tx.Exec(`ALTER TABLE projects ADD COLUMN evidence_generation INTEGER NOT NULL DEFAULT 0`); err != nil {
			return rollback(err)
		}
		if _, err := tx.Exec(`ALTER TABLE skill_candidates ADD COLUMN evidence_generation INTEGER NOT NULL DEFAULT 0`); err != nil {
			return rollback(err)
		}
	}
	if _, err := tx.Exec(`CREATE TABLE procedure_claims (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		evidence_id TEXT NOT NULL,
		procedure_key TEXT NOT NULL,
		content TEXT NOT NULL,
		outcome TEXT NOT NULL,
		generalizable INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE(project_id, evidence_id, procedure_key, content, outcome, generalizable),
		FOREIGN KEY(project_id) REFERENCES projects(project_id),
		FOREIGN KEY(evidence_id) REFERENCES evidence_refs(id)
	)`); err != nil {
		return rollback(err)
	}
	if _, err := tx.Exec("PRAGMA user_version = 3"); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}
