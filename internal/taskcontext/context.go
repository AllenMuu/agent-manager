// Package taskcontext assembles the stable inputs a role needs without
// coupling artifact storage to a particular agent runtime or memory provider.
package taskcontext

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/role"
)

type Bundle struct {
	ProjectRoot         string                              `json:"projectRoot"`
	TaskID              string                              `json:"taskId"`
	Role                role.ID                             `json:"role"`
	Artifacts           map[artifact.Kind]artifact.Document `json:"artifacts"`
	Missing             []artifact.Kind                     `json:"missing"`
	RepositoryKnowledge []KnowledgeFile                     `json:"repositoryKnowledge"`
	Skills              []catalog.Skill                     `json:"skills"`
	Memory              []MemoryItem                        `json:"memory"`
	MemoryState         memory.ProviderStatus               `json:"memoryState"`
	Warnings            []string                            `json:"warnings"`
}

type KnowledgeFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type MemoryItem struct {
	Scope   memory.Scope `json:"scope"`
	Content string       `json:"content"`
}

type Options struct {
	Project        string
	TaskID         string
	Library        string
	Contract       role.Contract
	Agent          string
	SelectedSkills []string
	Memory         memory.Searcher
	MemoryScopes   []memory.Scope
}

const maxKnowledgeFileSize = 64 * 1024

// Resolve builds an adapter-ready task context from repository guidance,
// artifacts, and compatible local Skills. It never invokes an agent runtime or
// writes to a provider.
func Resolve(project, taskID, library string, contract role.Contract) (Bundle, error) {
	return ResolveWithOptions(Options{Project: project, TaskID: taskID, Library: library, Contract: contract})
}

// ResolveForAgent applies the selected agent's compatibility declaration while
// preserving the same role/artifact assembly behavior as Resolve.
func ResolveForAgent(project, taskID, library string, contract role.Contract, agent string) (Bundle, error) {
	return ResolveWithOptions(Options{Project: project, TaskID: taskID, Library: library, Contract: contract, Agent: agent})
}

// ResolveWithOptions assembles bounded repository guidance, selected and
// agent-compatible Skills, optional relevant Memory, and role inputs. It does
// not invoke an agent runtime or write to any provider.
func ResolveWithOptions(options Options) (Bundle, error) {
	if err := options.Contract.Validate(); err != nil {
		return Bundle{}, err
	}
	store, err := artifact.NewStore(options.Project)
	if err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{
		ProjectRoot: store.ProjectRoot, TaskID: options.TaskID, Role: options.Contract.ID,
		Artifacts: map[artifact.Kind]artifact.Document{}, Missing: []artifact.Kind{},
		RepositoryKnowledge: []KnowledgeFile{}, Skills: []catalog.Skill{}, Memory: []MemoryItem{}, Warnings: []string{},
	}
	for _, kind := range options.Contract.Inputs {
		doc, _, loadErr := store.Load(options.TaskID, kind)
		if loadErr != nil {
			if errors.Is(loadErr, os.ErrNotExist) {
				bundle.Missing = append(bundle.Missing, kind)
				continue
			}
			return Bundle{}, loadErr
		}
		bundle.Artifacts[kind] = doc
	}
	bundle.RepositoryKnowledge, err = readRepositoryKnowledge(store.ProjectRoot)
	if err != nil {
		return Bundle{}, err
	}
	if options.Library != "" {
		skills, _, discoverErr := catalog.Discover(options.Library)
		if discoverErr != nil {
			return Bundle{}, fmt.Errorf("discover compatible Skills: %w", discoverErr)
		}
		selected := make(map[string]struct{}, len(options.SelectedSkills))
		for _, identifier := range options.SelectedSkills {
			selected[identifier] = struct{}{}
		}
		found := make(map[string]struct{}, len(options.SelectedSkills))
		for _, skill := range skills {
			if _, ok := selected[skill.Identifier]; !ok {
				continue
			}
			found[skill.Identifier] = struct{}{}
			compatible := options.Agent == "" || len(skill.Compatibility) == 0 || contains(skill.Compatibility, options.Agent) || contains(skill.Compatibility, "all")
			if !compatible {
				bundle.Warnings = append(bundle.Warnings, fmt.Sprintf("selected Skill %q does not declare compatibility with agent %q", skill.Identifier, options.Agent))
			}
			bundle.Skills = append(bundle.Skills, skill)
		}
		for _, identifier := range options.SelectedSkills {
			if _, ok := found[identifier]; !ok {
				return Bundle{}, fmt.Errorf("selected Skill %q was not found in the library", identifier)
			}
		}
	} else if len(options.SelectedSkills) > 0 {
		return Bundle{}, errors.New("a Skill library is required when selecting Skills")
	}
	if options.Memory != nil {
		bundle.MemoryState = options.Memory.Status()
		if bundle.MemoryState.Available {
			if !hasCapability(bundle.MemoryState.Capabilities, memory.CapabilityRead) || !hasCapability(bundle.MemoryState.Capabilities, memory.CapabilitySearch) {
				return Bundle{}, errors.New("Memory provider must declare read and search capabilities for context assembly")
			}
			scopes := options.MemoryScopes
			if len(scopes) == 0 {
				scopes = []memory.Scope{memory.ScopeProject}
			}
			query := memoryQuery(bundle.Artifacts)
			if query != "" {
				for _, scope := range scopes {
					if !hasScope(bundle.MemoryState.Scopes, scope) {
						return Bundle{}, fmt.Errorf("Memory provider does not support scope %q for context assembly", scope)
					}
					results, searchErr := options.Memory.Search(scope, query)
					if searchErr != nil {
						return Bundle{}, fmt.Errorf("search %s Memory context: %w", scope, searchErr)
					}
					for _, result := range results {
						if strings.TrimSpace(result) != "" {
							bundle.Memory = append(bundle.Memory, MemoryItem{Scope: scope, Content: result})
						}
					}
				}
			}
		}
	}
	return bundle, nil
}

func readRepositoryKnowledge(root string) ([]KnowledgeFile, error) {
	files := make([]KnowledgeFile, 0, 2)
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect repository knowledge %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("repository knowledge %s must be a direct regular file", path)
		}
		if info.Size() > maxKnowledgeFileSize {
			return nil, fmt.Errorf("repository knowledge %s exceeds %d bytes", path, maxKnowledgeFileSize)
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read repository knowledge %s: %w", path, err)
		}
		openedInfo, statErr := file.Stat()
		if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			_ = file.Close()
			if statErr != nil {
				return nil, fmt.Errorf("inspect opened repository knowledge %s: %w", path, statErr)
			}
			return nil, fmt.Errorf("repository knowledge %s changed while it was being opened", path)
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxKnowledgeFileSize+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read repository knowledge %s: %w", path, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close repository knowledge %s: %w", path, closeErr)
		}
		if len(content) > maxKnowledgeFileSize {
			return nil, fmt.Errorf("repository knowledge %s exceeds %d bytes", path, maxKnowledgeFileSize)
		}
		files = append(files, KnowledgeFile{Path: name, Content: string(content)})
	}
	return files, nil
}

func memoryQuery(artifacts map[artifact.Kind]artifact.Document) string {
	var parts []string
	for _, kind := range []artifact.Kind{artifact.Intent, artifact.Spec, artifact.Plan} {
		doc, ok := artifacts[kind]
		if !ok {
			continue
		}
		for _, field := range []string{"summary", "problem", "goals", "constraints", "decisions", "steps"} {
			appendQueryParts(&parts, doc.Values[field])
		}
	}
	query := strings.Join(parts, " ")
	if runes := []rune(query); len(runes) > 2000 {
		query = string(runes[:2000])
	}
	return strings.TrimSpace(query)
}

func appendQueryParts(parts *[]string, value any) {
	switch value := value.(type) {
	case string:
		if text := strings.TrimSpace(value); text != "" {
			*parts = append(*parts, text)
		}
	case []any:
		for _, item := range value {
			appendQueryParts(parts, item)
		}
	case map[string]any:
		for _, key := range []string{"decision", "description", "rationale", "content"} {
			appendQueryParts(parts, value[key])
		}
	}
}

func hasCapability(values []memory.Capability, wanted memory.Capability) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func hasScope(values []memory.Scope, wanted memory.Scope) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// PromoteLessons is the explicit bridge from a lessons artifact to shared
// Memory. The caller must supply confirmation; no task resolution path calls
// this function implicitly.
func PromoteLessons(doc artifact.Document, provider memory.Provider, scope memory.Scope, confirm func() bool) error {
	if provider == nil {
		return errors.New("Memory provider is required")
	}
	knowledge, err := FormatLessons(doc)
	if err != nil {
		return err
	}
	if confirm == nil || !confirm() {
		return errors.New("lessons promotion was not confirmed")
	}
	return provider.Promote(scope, knowledge)
}

// FormatLessons converts typed lesson items into the explicit text payload
// accepted by the current Memory provider interface.
func FormatLessons(doc artifact.Document) (string, error) {
	items, err := doc.LessonItems()
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", errors.New("lessons artifact contains no items")
	}
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s][%s][confidence=%s] %s", item.Type, item.Scope, item.Confidence, item.Content)
	}
	return b.String(), nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
