// Package artifact implements the agent-neutral task artifact protocol.
//
// Artifacts are deliberately represented as YAML maps at the storage boundary.
// This keeps the protocol forward compatible: fields introduced by a newer
// Agent Manager are retained when an older version validates or renders an
// artifact instead of being silently discarded.
package artifact

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const Version = "v1"

// Kind identifies one stage in the cross-agent SDLC artifact lifecycle.
type Kind string

const (
	Intent         Kind = "intent"
	Spec           Kind = "spec"
	Plan           Kind = "plan"
	Implementation Kind = "implementation"
	Verification   Kind = "verification"
	Lessons        Kind = "lessons"
)

var validKinds = map[Kind]bool{
	Intent: true, Spec: true, Plan: true, Implementation: true,
	Verification: true, Lessons: true,
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Envelope is the common metadata carried by every artifact. Document keeps
// the same fields in its Values map so unknown fields can round-trip.
type Envelope struct {
	Version   string    `yaml:"version" json:"version"`
	Kind      Kind      `yaml:"kind" json:"kind"`
	ID        string    `yaml:"id" json:"id"`
	CreatedAt time.Time `yaml:"created_at" json:"created_at"`
	Project   Project   `yaml:"project" json:"project"`
	Source    Source    `yaml:"source" json:"source"`
	Links     Links     `yaml:"links" json:"links"`
	Metadata  any       `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

type Project struct {
	Root string `yaml:"root,omitempty" json:"root,omitempty"`
}

type Source struct {
	Actor string `yaml:"actor,omitempty" json:"actor,omitempty"`
	Agent string `yaml:"agent,omitempty" json:"agent,omitempty"`
}

type Links struct {
	Parent string `yaml:"parent,omitempty" json:"parent,omitempty"`
	Task   string `yaml:"task,omitempty" json:"task,omitempty"`
}

// Document is a parsed artifact. Values contains both protocol-defined and
// future fields and is therefore safe to round-trip without data loss.
type Document struct {
	Values map[string]any `json:"values"`
}

type LessonItem struct {
	Type       string `yaml:"type" json:"type"`
	Scope      string `yaml:"scope" json:"scope"`
	Content    string `yaml:"content" json:"content"`
	Confidence string `yaml:"confidence" json:"confidence"`
}

// New creates a valid common envelope with an empty payload.
func New(kind Kind, id string, projectRoot string, now time.Time) Document {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return Document{Values: map[string]any{
		"version":    Version,
		"kind":       string(kind),
		"id":         id,
		"created_at": now.UTC().Format(time.RFC3339Nano),
		"project":    map[string]any{"root": projectRoot},
		"source":     map[string]any{"actor": "human"},
		"links":      map[string]any{"parent": nil, "task": id},
		"metadata":   map[string]any{},
	}}
}

// Parse decodes and validates one YAML artifact. Unknown keys are accepted
// and retained in Values for forward compatibility.
func Parse(data []byte) (Document, error) {
	var values map[string]any
	if err := yaml.Unmarshal(data, &values); err != nil {
		return Document{}, fmt.Errorf("parse artifact YAML: %w", err)
	}
	if values == nil {
		return Document{}, errors.New("artifact must be a YAML mapping")
	}
	doc := Document{Values: values}
	if err := doc.Validate(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// Load reads and validates an artifact file.
func Load(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, fmt.Errorf("read artifact %s: %w", path, err)
	}
	doc, err := Parse(data)
	if err != nil {
		return Document{}, fmt.Errorf("validate artifact %s: %w", path, err)
	}
	return doc, nil
}

// YAML returns a deterministic YAML serialization of the complete document.
func (d Document) YAML() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return yaml.Marshal(d.Values)
}

// Render returns a human-readable Markdown inspection view with the complete
// canonical artifact embedded as YAML.
func (d Document) Render() (string, error) {
	b, err := d.YAML()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("# %s: %s\n\n```yaml\n%s```\n", d.Kind(), d.ID(), b), nil
}

func (d Document) Kind() Kind { return Kind(stringValue(d.Values["kind"])) }
func (d Document) ID() string { return stringValue(d.Values["id"]) }

// Get returns a protocol or extension field.
func (d Document) Get(name string) (any, bool) {
	value, ok := d.Values[name]
	return value, ok
}

// Set updates a field while retaining all other fields.
func (d *Document) Set(name string, value any) {
	if d.Values == nil {
		d.Values = map[string]any{}
	}
	d.Values[name] = value
}

// LessonItems decodes the typed lessons payload for explicit promotion into a
// Memory provider. It does not perform that promotion itself.
func (d Document) LessonItems() ([]LessonItem, error) {
	if d.Kind() != Lessons {
		return nil, fmt.Errorf("artifact %q is not a lessons artifact", d.Kind())
	}
	raw, ok := listValue(d.Values["items"])
	if !ok {
		return nil, errors.New("lessons items must be a list")
	}
	items := make([]LessonItem, 0, len(raw))
	for i, value := range raw {
		m, ok := mapValue(value)
		if !ok {
			return nil, fmt.Errorf("lessons item %d must be a mapping", i)
		}
		items = append(items, LessonItem{
			Type: stringValue(m["type"]), Scope: stringValue(m["scope"]),
			Content: stringValue(m["content"]), Confidence: stringValue(m["confidence"]),
		})
	}
	return items, nil
}

// Validate enforces the common envelope and the kind-specific primitive
// constraints. It intentionally does not reject unknown fields.
func (d Document) Validate() error {
	if d.Values == nil {
		return errors.New("artifact must be a YAML mapping")
	}
	if got := stringValue(d.Values["version"]); got != Version {
		return fmt.Errorf("unsupported artifact version %q", got)
	}
	kind := Kind(stringValue(d.Values["kind"]))
	if !validKinds[kind] {
		return fmt.Errorf("unsupported artifact kind %q", kind)
	}
	id := stringValue(d.Values["id"])
	if !safeID.MatchString(id) {
		return fmt.Errorf("unsafe artifact id %q", id)
	}
	if !hasTimestamp(d.Values["created_at"]) {
		return errors.New("artifact created_at is required and must be an RFC3339 timestamp")
	}
	project, ok := mapValue(d.Values["project"])
	if !ok || !hasRequiredText(project["root"]) {
		return errors.New("artifact project.root is required and must be a string")
	}
	source, ok := mapValue(d.Values["source"])
	if !ok || !hasRequiredText(source["actor"]) {
		return errors.New("artifact source.actor is required and must be a string")
	}
	if err := optionalString(source, "agent", "artifact source.agent"); err != nil {
		return err
	}
	links, ok := mapValue(d.Values["links"])
	if !ok || stringValue(links["task"]) != id {
		return fmt.Errorf("artifact links.task must match id %q", id)
	}
	if parent := links["parent"]; parent != nil {
		if parentID := stringValue(parent); !safeID.MatchString(parentID) || parentID == "." || parentID == ".." {
			return errors.New("artifact links.parent must be a safe identifier or null")
		}
	}
	if metadata, ok := d.Values["metadata"]; ok && metadata != nil {
		if _, ok := mapValue(metadata); !ok {
			return errors.New("artifact metadata must be a mapping")
		}
	}
	return validatePayload(kind, d.Values)
}

func validatePayload(kind Kind, values map[string]any) error {
	switch kind {
	case Intent:
		if !hasRequiredText(values["summary"]) {
			return errors.New("intent summary is required and must be a string")
		}
		for _, field := range []string{"problem", "risk_level"} {
			if err := optionalString(values, field, "intent "+field); err != nil {
				return err
			}
		}
		for _, field := range []string{"goals", "non_goals", "constraints", "acceptance_criteria"} {
			if err := stringListField(values, field, "intent "+field, false); err != nil {
				return err
			}
		}
	case Spec:
		if err := objectListField(values, "decisions", "spec decisions", true, []string{"id", "decision", "rationale"}); err != nil {
			return err
		}
		for _, field := range []string{"interfaces", "data_models", "compatibility", "open_questions"} {
			if err := listField(values, field, "spec "+field, false); err != nil {
				return err
			}
		}
	case Plan:
		if err := objectListField(values, "steps", "plan steps", true, []string{"id", "description", "verification"}); err != nil {
			return err
		}
		steps, _ := listValue(values["steps"])
		for i, raw := range steps {
			step, _ := mapValue(raw)
			for _, field := range []string{"dependencies", "files"} {
				if err := stringListField(step, field, fmt.Sprintf("plan step %d %s", i, field), false); err != nil {
					return err
				}
			}
		}
	case Implementation:
		if !hasRequiredText(values["summary"]) {
			return errors.New("implementation summary is required and must be a string")
		}
		for _, field := range []string{"agent", "base_ref", "head_ref"} {
			if err := optionalString(values, field, "implementation "+field); err != nil {
				return err
			}
		}
		for _, field := range []string{"commits", "changed_files"} {
			if err := stringListField(values, field, "implementation "+field, false); err != nil {
				return err
			}
		}
	case Verification:
		status := stringValue(values["status"])
		if status != "pass" && status != "fail" && status != "partial" {
			return fmt.Errorf("verification status must be pass, fail, or partial; got %q", status)
		}
		if err := objectListField(values, "checks", "verification checks", status == "pass", []string{"name", "status"}); err != nil {
			return err
		}
		checks, _ := listValue(values["checks"])
		for i, raw := range checks {
			check, _ := mapValue(raw)
			checkStatus := stringValue(check["status"])
			if checkStatus != "pass" && checkStatus != "fail" && checkStatus != "partial" {
				return fmt.Errorf("verification check %d status must be pass, fail, or partial", i)
			}
			for _, field := range []string{"command", "evidence"} {
				if err := optionalString(check, field, fmt.Sprintf("verification check %d %s", i, field)); err != nil {
					return err
				}
			}
		}
		if err := stringListField(values, "risks", "verification risks", false); err != nil {
			return err
		}
	case Lessons:
		items, ok := listValue(values["items"])
		if !ok {
			return errors.New("lessons items must be a list")
		}
		for i, raw := range items {
			item, ok := mapValue(raw)
			if !ok {
				return fmt.Errorf("lessons item %d must be a mapping", i)
			}
			for _, field := range []string{"type", "scope", "content", "confidence"} {
				if !hasRequiredText(item[field]) {
					return fmt.Errorf("lessons item %d requires %s", i, field)
				}
			}
			switch stringValue(item["type"]) {
			case "decision", "constraint", "lesson", "known_issue":
			default:
				return fmt.Errorf("lessons item %d has unsupported type %q", i, stringValue(item["type"]))
			}
			switch stringValue(item["confidence"]) {
			case "low", "medium", "high":
			default:
				return fmt.Errorf("lessons item %d has unsupported confidence %q", i, stringValue(item["confidence"]))
			}
		}
	}
	return nil
}

func optionalString(values map[string]any, field, label string) error {
	if value, ok := values[field]; ok && value != nil {
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", label)
		}
	}
	return nil
}

func listField(values map[string]any, field, label string, nonempty bool) error {
	value, exists := values[field]
	if !exists {
		if nonempty {
			return fmt.Errorf("%s is required and must be a nonempty list", label)
		}
		return nil
	}
	items, ok := listValue(value)
	if !ok {
		return fmt.Errorf("%s must be a list", label)
	}
	if nonempty && len(items) == 0 {
		return fmt.Errorf("%s must be a nonempty list", label)
	}
	return nil
}

func stringListField(values map[string]any, field, label string, nonempty bool) error {
	if err := listField(values, field, label, nonempty); err != nil {
		return err
	}
	if _, exists := values[field]; !exists {
		return nil
	}
	items, _ := listValue(values[field])
	for i, item := range items {
		if _, ok := item.(string); !ok {
			return fmt.Errorf("%s item %d must be a string", label, i)
		}
	}
	return nil
}

func objectListField(values map[string]any, field, label string, nonempty bool, required []string) error {
	if err := listField(values, field, label, nonempty); err != nil {
		return err
	}
	if _, exists := values[field]; !exists {
		return nil
	}
	items, _ := listValue(values[field])
	for i, item := range items {
		object, ok := mapValue(item)
		if !ok {
			return fmt.Errorf("%s item %d must be a mapping", label, i)
		}
		for _, key := range required {
			if !hasRequiredText(object[key]) {
				return fmt.Errorf("%s item %d requires string %s", label, i, key)
			}
		}
	}
	return nil
}

func hasRequiredText(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}

func stringValue(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case time.Time:
		return value.Format(time.RFC3339Nano)
	default:
		return ""
	}
}

func hasTimestamp(value any) bool {
	if timestamp, ok := value.(time.Time); ok {
		return !timestamp.IsZero()
	}
	text := stringValue(value)
	if text == "" {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, text)
	return err == nil
}

func mapValue(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func listValue(value any) ([]any, bool) {
	switch items := value.(type) {
	case []any:
		return items, true
	case []string:
		converted := make([]any, len(items))
		for i, item := range items {
			converted[i] = item
		}
		return converted, true
	case []map[string]string:
		converted := make([]any, len(items))
		for i, item := range items {
			m := make(map[string]any, len(item))
			for key, value := range item {
				m[key] = value
			}
			converted[i] = m
		}
		return converted, true
	case []map[string]any:
		converted := make([]any, len(items))
		for i, item := range items {
			converted[i] = item
		}
		return converted, true
	default:
		return nil, false
	}
}

// Store is the deterministic local task/artifact store rooted at a project.
type Store struct{ ProjectRoot string }

func NewStore(project string) (Store, error) {
	if project == "" {
		project = "."
	}
	abs, err := filepath.Abs(project)
	if err != nil {
		return Store{}, fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Store{}, fmt.Errorf("inspect project root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Store{}, fmt.Errorf("project root %s must be a real directory", abs)
	}
	return Store{ProjectRoot: abs}, nil
}

func (s Store) TasksRoot() string { return filepath.Join(s.ProjectRoot, ".agents", "tasks") }

func (s Store) TaskDir(taskID string) (string, error) {
	if err := validateID(taskID); err != nil {
		return "", err
	}
	return filepath.Join(s.TasksRoot(), taskID), nil
}

// Init creates the task directory and its initial intent artifact. It refuses
// to overwrite an existing task.
func (s Store) Init(taskID string, intent Document) (string, error) {
	dir, err := s.TaskDir(taskID)
	if err != nil {
		return "", err
	}
	if intent.Kind() != Intent {
		return "", fmt.Errorf("task initialization requires an intent artifact")
	}
	if intent.ID() != taskID {
		return "", fmt.Errorf("intent id %q does not match task id %q", intent.ID(), taskID)
	}
	if err := intent.Validate(); err != nil {
		return "", err
	}
	if _, err := os.Lstat(dir); err == nil {
		return "", fmt.Errorf("task %q already exists", taskID)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect task directory: %w", err)
	}
	if _, err := s.taskDirectory(taskID, true); err != nil {
		return "", err
	}
	path := filepath.Join(dir, string(Intent)+".yaml")
	if err := writeFile(path, intent); err != nil {
		return "", err
	}
	return path, nil
}

// Save writes an artifact into an existing task directory. It does not execute
// any artifact content and only accepts known kinds and safe task IDs.
func (s Store) Save(taskID string, doc Document) (string, error) {
	dir, err := s.taskDirectory(taskID, false)
	if err != nil {
		return "", err
	}
	if doc.ID() == "" {
		return "", errors.New("artifact id is required")
	}
	if doc.ID() != taskID {
		return "", fmt.Errorf("artifact id %q does not match task id %q", doc.ID(), taskID)
	}
	if err := doc.Validate(); err != nil {
		return "", err
	}
	path := filepath.Join(dir, string(doc.Kind())+".yaml")
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("artifact destination %s must be a direct regular file", path)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect artifact destination: %w", err)
	}
	if err := writeFile(path, doc); err != nil {
		return "", err
	}
	return path, nil
}

type Entry struct {
	Kind Kind
	Path string
}

func (s Store) List(taskID string) ([]Entry, error) {
	dir, err := s.taskDirectory(taskID, false)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list task artifacts: %w", err)
	}
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		kind := Kind(strings.TrimSuffix(entry.Name(), ".yaml"))
		if !validKinds[kind] {
			continue
		}
		result = append(result, Entry{Kind: kind, Path: filepath.Join(dir, entry.Name())})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Kind < result[j].Kind })
	return result, nil
}

func (s Store) Load(taskID string, kind Kind) (Document, string, error) {
	dir, err := s.taskDirectory(taskID, false)
	if err != nil {
		return Document{}, "", err
	}
	if !validKinds[kind] {
		return Document{}, "", fmt.Errorf("unsupported artifact kind %q", kind)
	}
	path := filepath.Join(dir, string(kind)+".yaml")
	info, err := os.Lstat(path)
	if err != nil {
		return Document{}, "", fmt.Errorf("inspect artifact %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Document{}, "", fmt.Errorf("artifact %s must be a direct regular file", path)
	}
	doc, err := Load(path)
	if err == nil && doc.ID() != taskID {
		return Document{}, "", fmt.Errorf("artifact id %q does not match task id %q", doc.ID(), taskID)
	}
	return doc, path, err
}

func (s Store) taskDirectory(taskID string, create bool) (string, error) {
	if err := validateID(taskID); err != nil {
		return "", err
	}
	current := s.ProjectRoot
	for _, component := range []string{".agents", "tasks", taskID} {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if !create {
				return "", fmt.Errorf("inspect task directory %s: %w", current, err)
			}
			if err := os.Mkdir(current, 0o755); err != nil && !os.IsExist(err) {
				return "", fmt.Errorf("create task directory %s: %w", current, err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return "", fmt.Errorf("inspect task directory %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("task path component %s must be a real directory", current)
		}
	}
	return current, nil
}

func validateID(id string) error {
	if !safeID.MatchString(id) || id == "." || id == ".." {
		return fmt.Errorf("unsafe task id %q", id)
	}
	return nil
}

func writeFile(path string, doc Document) error {
	data, err := doc.YAML()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".artifact-")
	if err != nil {
		return fmt.Errorf("create artifact temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := bytes.NewReader(data).WriteTo(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write artifact temp file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("publish artifact: %w", err)
	}
	return nil
}
