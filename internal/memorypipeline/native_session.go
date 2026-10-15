package memorypipeline

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/run"
)

const (
	maxNativeSessionBytes   = 32 << 20
	maxNativeSessionFiles   = 10000
	maxNativeSessionLines   = 100000
	maxNativeSessionEntries = 20000
)

// NativeSessionProvider adapts read-only Codex rollout or Pi session JSONL
// files. It keeps only a bounded summary and source digest in the derived
// database; transcript text remains in the runtime's native file.
type NativeSessionProvider struct {
	runtime     string
	sourceDir   string
	projectID   string
	projectRoot string
}

func NewCodexRolloutProvider(sourceDir, projectID, projectRoot string) (*NativeSessionProvider, error) {
	return newNativeSessionProvider("codex", sourceDir, projectID, projectRoot)
}

func NewPiSessionProvider(sourceDir, projectID, projectRoot string) (*NativeSessionProvider, error) {
	return newNativeSessionProvider("pi", sourceDir, projectID, projectRoot)
}

func newNativeSessionProvider(runtime, sourceDir, projectID, projectRoot string) (*NativeSessionProvider, error) {
	if (runtime != "codex" && runtime != "pi") || strings.TrimSpace(sourceDir) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("%w: runtime, source directory, project identity, and project root are required", ErrInvalidEvidence)
	}
	root, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("resolve native session directory: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve native session directory: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: native session source must be a real directory", ErrInvalidEvidence)
	}
	project, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve native session project: %w", err)
	}
	project, err = filepath.EvalSymlinks(project)
	if err != nil {
		return nil, fmt.Errorf("resolve native session project: %w", err)
	}
	projectInfo, err := os.Lstat(project)
	if err != nil || !projectInfo.IsDir() || projectInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: native session project must be a real directory", ErrInvalidEvidence)
	}
	return &NativeSessionProvider{runtime: runtime, sourceDir: root, projectID: projectID, projectRoot: filepath.Clean(project)}, nil
}

func (p *NativeSessionProvider) Discover(ctx context.Context, projectID string) ([]RunEvidenceRef, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if p == nil || projectID != p.projectID {
		return nil, ErrEvidenceNotFound
	}
	paths, err := nativeSessionPaths(p.sourceDir)
	if err != nil {
		return nil, err
	}
	refs := make([]RunEvidenceRef, 0)
	for _, path := range paths {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		evidence, complete, err := p.readPath(path)
		if err != nil {
			return nil, err
		}
		if !complete || !samePath(evidenceProjectRoot(evidence), p.projectRoot) {
			continue
		}
		ref, err := referenceFor(evidence)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].CreatedAt.Equal(refs[j].CreatedAt) {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].CreatedAt.Before(refs[j].CreatedAt)
	})
	return refs, nil
}

func (p *NativeSessionProvider) Read(ctx context.Context, ref RunEvidenceRef) (RunEvidence, error) {
	if err := contextErr(ctx); err != nil {
		return RunEvidence{}, err
	}
	if p == nil || ref.ProjectID != p.projectID || ref.Runtime != p.runtime || !nativeSourceReference(ref.SourceRef, p.runtime, ref.NativeRunID) {
		return RunEvidence{}, ErrEvidenceNotFound
	}
	paths, err := nativeSessionPaths(p.sourceDir)
	if err != nil {
		return RunEvidence{}, err
	}
	for _, path := range paths {
		if err := contextErr(ctx); err != nil {
			return RunEvidence{}, err
		}
		evidence, complete, err := p.readPath(path)
		if err != nil {
			return RunEvidence{}, err
		}
		if evidence.Ref.NativeRunID != ref.NativeRunID || !samePath(evidenceProjectRoot(evidence), p.projectRoot) {
			continue
		}
		if !complete {
			return RunEvidence{}, ErrEvidenceChanged
		}
		computed, err := referenceFor(evidence)
		if err != nil {
			return RunEvidence{}, err
		}
		if !sameReference(ref, computed) {
			return RunEvidence{}, ErrEvidenceChanged
		}
		evidence.Ref = computed
		return evidence, nil
	}
	return RunEvidence{}, ErrEvidenceNotFound
}

// evidenceProjectRoot is carried only while parsing a native source; it is not
// included in normalized evidence or persisted in the project database.
func evidenceProjectRoot(evidence RunEvidence) string { return evidence.projectRoot }

func nativeSessionPaths(root string) ([]string, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: native session directory is unavailable", ErrInvalidEvidence)
	}
	paths := make([]string, 0)
	entries := 0
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > maxNativeSessionEntries {
			return fmt.Errorf("%w: native session directory exceeds %d entries", ErrInvalidEvidence, maxNativeSessionEntries)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if len(paths) >= maxNativeSessionFiles {
			return fmt.Errorf("%w: native session directory exceeds %d files", ErrInvalidEvidence, maxNativeSessionFiles)
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list native sessions: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

func (p *NativeSessionProvider) readPath(path string) (RunEvidence, bool, error) {
	data, err := readNativeSessionFile(path)
	if err != nil {
		return RunEvidence{}, false, err
	}
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var evidence RunEvidence
	var projectRoot string
	var complete bool
	if p.runtime == "codex" {
		evidence, projectRoot, complete, err = parseCodexRollout(data, p.projectID, p.projectRoot, digest)
	} else {
		evidence, projectRoot, complete, err = parsePiSession(data, p.projectID, p.projectRoot, digest)
	}
	if err != nil {
		return RunEvidence{}, false, err
	}
	if projectRoot != "" && nativeProjectMatches(projectRoot, p.projectRoot) {
		evidence.projectRoot = p.projectRoot
	}
	return evidence, complete, nil
}

func readNativeSessionFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect native session: %w", err)
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() <= 0 || before.Size() > maxNativeSessionBytes {
		return nil, fmt.Errorf("%w: native session file is not regular or exceeds %d bytes", ErrInvalidEvidence, maxNativeSessionBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open native session: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: native session changed while opening", ErrEvidenceChanged)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxNativeSessionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read native session: %w", err)
	}
	if len(data) > maxNativeSessionBytes {
		return nil, fmt.Errorf("%w: native session exceeds %d bytes", ErrInvalidEvidence, maxNativeSessionBytes)
	}
	afterOpen, statErr := file.Stat()
	afterPath, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || !os.SameFile(opened, afterOpen) || !os.SameFile(opened, afterPath) ||
		opened.Size() != afterOpen.Size() || opened.ModTime() != afterOpen.ModTime() || afterOpen.Size() != int64(len(data)) {
		return nil, fmt.Errorf("%w: native session changed while reading", ErrEvidenceChanged)
	}
	return data, nil
}

type nativeLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
	Raw       json.RawMessage `json:"-"`
}

func scanNativeLines(data []byte) ([]nativeLine, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxNativeSessionBytes)
	lines := make([]nativeLine, 0)
	for scanner.Scan() {
		if len(lines) >= maxNativeSessionLines {
			return nil, fmt.Errorf("%w: native session exceeds %d records", ErrInvalidEvidence, maxNativeSessionLines)
		}
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var line nativeLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return nil, fmt.Errorf("%w: malformed native session record", ErrInvalidEvidence)
		}
		if len(line.Payload) == 0 {
			line.Raw = append(json.RawMessage(nil), scanner.Bytes()...)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read native session records: %w", err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: native session is empty", ErrInvalidEvidence)
	}
	return lines, nil
}

func parseCodexRollout(data []byte, projectID, expectedProjectRoot, digest string) (RunEvidence, string, bool, error) {
	lines, err := scanNativeLines(data)
	if err != nil {
		return RunEvidence{}, "", false, err
	}
	if lines[0].Type != "session_meta" {
		return RunEvidence{}, "", false, fmt.Errorf("%w: Codex rollout has no session metadata", ErrInvalidEvidence)
	}
	var metadata struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		Timestamp string `json:"timestamp"`
		Meta      struct {
			ID        string `json:"id"`
			SessionID string `json:"session_id"`
			CWD       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(lines[0].Payload, &metadata); err != nil {
		return RunEvidence{}, "", false, fmt.Errorf("%w: invalid Codex session metadata", ErrInvalidEvidence)
	}
	id := metadata.SessionID
	if id == "" {
		id = metadata.ID
	}
	if id == "" {
		id = metadata.Meta.SessionID
	}
	if id == "" {
		id = metadata.Meta.ID
	}
	cwd := metadata.CWD
	if cwd == "" {
		cwd = metadata.Meta.CWD
	}
	created, err := parseNativeTimestamp(metadata.Timestamp, metadata.Meta.Timestamp, lines[0].Timestamp)
	if err != nil || id == "" || cwd == "" {
		return RunEvidence{}, "", false, fmt.Errorf("%w: Codex session identity, working directory, or timestamp is missing", ErrInvalidEvidence)
	}
	if !nativeProjectMatches(cwd, expectedProjectRoot) {
		return RunEvidence{}, cwd, false, nil
	}
	lastUser, lastTerminal := -1, -1
	terminalStatus := ""
	lastUpdated := created
	for index, line := range lines[1:] {
		index++
		var payload struct {
			Type string `json:"type"`
			Role string `json:"role"`
		}
		if err := json.Unmarshal(line.Payload, &payload); err != nil {
			continue
		}
		switch line.Type {
		case "event_msg":
			switch payload.Type {
			case "user_message":
				lastUser = index
			case "task_started":
				lastTerminal = -1
				terminalStatus = ""
			case "task_complete":
				lastTerminal = index
				terminalStatus = string(run.Completed)
				if parsed, parseErr := parseNativeTimestamp(line.Timestamp); parseErr == nil {
					lastUpdated = parsed
				}
			case "turn_aborted", "shutdown_complete":
				lastTerminal = index
				terminalStatus = string(run.Terminated)
				if parsed, parseErr := parseNativeTimestamp(line.Timestamp); parseErr == nil {
					lastUpdated = parsed
				}
			}
		case "response_item":
			if payload.Type == "message" && payload.Role == "user" {
				lastUser = index
			}
		}
	}
	complete := lastTerminal >= 0 && lastTerminal > lastUser
	if !complete {
		terminalStatus = string(run.Active)
	}
	return RunEvidence{
		Ref:    RunEvidenceRef{ProjectID: projectID, Runtime: "codex", NativeRunID: id, SourceRef: "codex://sessions/" + id, CreatedAt: created, Status: ImportCompleted},
		Status: terminalStatus, UpdatedAt: lastUpdated, SourceDigest: digest,
		SummaryText: fmt.Sprintf("Codex rollout %s ended as %s with %d native records.", id, terminalStatus, len(lines)),
	}, cwd, complete, nil
}

func parsePiSession(data []byte, projectID, expectedProjectRoot, digest string) (RunEvidence, string, bool, error) {
	lines, err := scanNativeLines(data)
	if err != nil {
		return RunEvidence{}, "", false, err
	}
	var header struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		CWD       string `json:"cwd"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(lines[0].PayloadOrSelf(), &header); err != nil || header.Type != "session" {
		return RunEvidence{}, "", false, fmt.Errorf("%w: Pi session has no valid session header", ErrInvalidEvidence)
	}
	created, err := parseNativeTimestamp(header.Timestamp)
	if err != nil || header.ID == "" || header.CWD == "" {
		return RunEvidence{}, "", false, fmt.Errorf("%w: Pi session identity, working directory, or timestamp is missing", ErrInvalidEvidence)
	}
	if !nativeProjectMatches(header.CWD, expectedProjectRoot) {
		return RunEvidence{}, header.CWD, false, nil
	}
	lastUser, lastAssistant := -1, -1
	lastReason := ""
	lastUpdated := created
	for index, line := range lines[1:] {
		index++
		if line.Type != "message" {
			continue
		}
		var payload struct {
			Role       string `json:"role"`
			StopReason string `json:"stopReason"`
			Timestamp  string `json:"timestamp"`
			Message    struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line.PayloadOrSelf(), &payload); err != nil {
			continue
		}
		role := payload.Role
		reason := payload.StopReason
		if payload.Message.Role != "" {
			role = payload.Message.Role
		}
		if payload.Message.StopReason != "" {
			reason = payload.Message.StopReason
		}
		switch role {
		case "user":
			lastUser = index
		case "assistant":
			lastAssistant = index
			lastReason = reason
			if parsed, parseErr := parseNativeTimestamp(payload.Timestamp, line.Timestamp); parseErr == nil {
				lastUpdated = parsed
			}
		}
	}
	complete := lastAssistant >= 0 && lastAssistant > lastUser && (lastReason == "stop" || lastReason == "length")
	return RunEvidence{
		Ref:    RunEvidenceRef{ProjectID: projectID, Runtime: "pi", NativeRunID: header.ID, SourceRef: "pi://sessions/" + header.ID, CreatedAt: created, Status: ImportCompleted},
		Status: string(run.Completed), UpdatedAt: lastUpdated, SourceDigest: digest,
		SummaryText: fmt.Sprintf("Pi session %s completed with %d native records.", header.ID, len(lines)),
	}, header.CWD, complete, nil
}

func (line nativeLine) PayloadOrSelf() json.RawMessage {
	if len(line.Payload) != 0 {
		return line.Payload
	}
	return line.Raw
}

func parseNativeTimestamp(values ...string) (time.Time, error) {
	for _, value := range values {
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err == nil && !parsed.IsZero() {
			return parsed.UTC(), nil
		}
		for _, layout := range []string{"2006-01-02T15-04-05.999999999", "2006-01-02T15-04-05"} {
			parsed, err := time.Parse(layout, value)
			if err == nil && !parsed.IsZero() {
				return parsed.UTC(), nil
			}
		}
	}
	return time.Time{}, errors.New("native session timestamp is invalid")
}

func nativeSourceReference(ref, runtime, nativeID string) bool {
	return ref == runtime+"://sessions/"+nativeID
}

func nativeProjectMatches(nativeRoot, expectedRoot string) bool {
	if !filepath.IsAbs(nativeRoot) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(nativeRoot)
	return err == nil && filepath.Clean(resolved) == filepath.Clean(expectedRoot)
}
