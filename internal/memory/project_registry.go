package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ProjectRegistry stores explicit canonical directory mappings to opaque stable
// IDs. Opening and lookup are read-only; registration and relocation are
// separate confirmed operator operations, outside the Skill journal.
type ProjectRegistry struct {
	root, name     string
	identity       os.FileInfo
	canonicalRoots []string
}
type ProjectIdentity struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
}

func OpenProjectRegistry(path string, canonicalRoots ...string) (*ProjectRegistry, error) {
	if !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\n\r") {
		return nil, ErrInvalidInput
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrInvalidInput
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, ErrUnavailable
	}
	dir, err := openStoreDirectory(root)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer dir.Close()
	identity, err := dir.Stat()
	if err != nil {
		return nil, ErrUnavailable
	}
	r := &ProjectRegistry{root: root, name: filepath.Base(path), identity: identity, canonicalRoots: append([]string{root}, canonicalRoots...)}
	if strings.EqualFold(r.name, "memory.json") || strings.EqualFold(r.name, "memory.lock") {
		return nil, ErrInvalidInput
	}
	_, err = r.load(dir)
	return r, err
}
func (r *ProjectRegistry) directory() (*os.File, error) {
	if r == nil {
		return nil, ErrUnavailable
	}
	dir, err := openStoreDirectory(r.root)
	if err != nil {
		return nil, ErrUnavailable
	}
	info, err := dir.Stat()
	if err != nil || !os.SameFile(info, r.identity) {
		dir.Close()
		return nil, ErrUnavailable
	}
	return dir, nil
}
func (r *ProjectRegistry) load(dir *os.File) ([]ProjectIdentity, error) {
	if err := r.rejectStateAliases(dir); err != nil {
		return nil, err
	}
	data, err := readStoreFile(dir, r.name)
	if os.IsNotExist(err) {
		return []ProjectIdentity{}, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	if !utf8.Valid(data) || validateJSONUnicode(data) != nil {
		return nil, ErrUnavailable
	}
	var projects []ProjectIdentity
	if json.Unmarshal(data, &projects) != nil {
		return nil, ErrUnavailable
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	directoryInfos := []os.FileInfo{}
	for _, p := range projects {
		if validateStructuredToken("project ID", p.ID) != nil || !utf8.ValidString(p.Directory) || strings.ContainsAny(p.Directory, "\x00\n\r") || !filepath.IsAbs(p.Directory) || filepath.Clean(p.Directory) != p.Directory || ids[p.ID] || paths[p.Directory] {
			return nil, ErrUnavailable
		}
		if info, err := os.Stat(p.Directory); err == nil {
			for _, other := range directoryInfos {
				if os.SameFile(info, other) {
					return nil, ErrUnavailable
				}
			}
			directoryInfos = append(directoryInfos, info)
		}
		ids[p.ID] = true
		paths[p.Directory] = true
	}
	return projects, nil
}
func (r *ProjectRegistry) rejectStateAliases(dir *os.File) error {
	info, err := inspectStoreFile(dir, r.name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return ErrUnavailable
	}
	for _, root := range r.canonicalRoots {
		canonical, err := filepath.EvalSymlinks(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return ErrUnavailable
		}
		storage, err := openStoreDirectory(canonical)
		if err != nil {
			return ErrUnavailable
		}
		for _, name := range []string{"memory.json", "memory.lock"} {
			reserved, err := inspectStoreFile(storage, name)
			if err == nil && os.SameFile(info, reserved) {
				storage.Close()
				return ErrInvalidInput
			}
			if err != nil && !os.IsNotExist(err) {
				storage.Close()
				return ErrUnavailable
			}
		}
		storage.Close()
	}
	return nil
}

func projectDirectory(path string) (string, error) {
	if !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\n\r") {
		return "", ErrInvalidInput
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", ErrInvalidInput
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidInput
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", ErrInvalidInput
	}
	return canonical, nil
}
func (r *ProjectRegistry) Lookup(path string) (ProjectIdentity, error) {
	path, err := projectDirectory(path)
	if err != nil {
		return ProjectIdentity{}, err
	}
	dir, err := r.directory()
	if err != nil {
		return ProjectIdentity{}, err
	}
	defer dir.Close()
	projects, err := r.load(dir)
	if err != nil {
		return ProjectIdentity{}, err
	}
	info, _ := os.Stat(path)
	for _, p := range projects {
		if sameProjectDirectory(p.Directory, path, info) {
			return p, nil
		}
	}
	return ProjectIdentity{}, ErrNotFound
}

// ProjectMappingPreview binds the displayed canonical path and opaque project
// ID to the directory identity and prior mapping observed before confirmation.
type ProjectMappingPlan struct {
	Operation string `json:"operation"`
	ID        string `json:"id,omitempty"`
	Directory string `json:"directory"`
}
type ProjectMappingPreview struct {
	registry *ProjectRegistry
	plan     ProjectMappingPlan
	identity os.FileInfo
	prior    ProjectIdentity
}

func (p ProjectMappingPreview) Plan() ProjectMappingPlan { return p.plan }
func (r *ProjectRegistry) PreviewRegister(ctx context.Context, path string) (ProjectMappingPreview, error) {
	return r.preview(ctx, "", path)
}
func (r *ProjectRegistry) PreviewRelocate(ctx context.Context, id, path string) (ProjectMappingPreview, error) {
	if validateStructuredToken("project ID", id) != nil {
		return ProjectMappingPreview{}, ErrInvalidInput
	}
	return r.preview(ctx, id, path)
}
func (r *ProjectRegistry) preview(ctx context.Context, id, path string) (ProjectMappingPreview, error) {
	if err := operationContext(ctx); err != nil {
		return ProjectMappingPreview{}, err
	}
	path, err := projectDirectory(path)
	if err != nil {
		return ProjectMappingPreview{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return ProjectMappingPreview{}, ErrInvalidInput
	}
	dir, err := r.directory()
	if err != nil {
		return ProjectMappingPreview{}, err
	}
	defer dir.Close()
	projects, err := r.load(dir)
	if err != nil {
		return ProjectMappingPreview{}, err
	}
	preview := ProjectMappingPreview{registry: r, plan: ProjectMappingPlan{Operation: "register", ID: id, Directory: path}, identity: info}
	if id != "" {
		preview.plan.Operation = "relocate"
	}
	for _, p := range projects {
		if sameProjectDirectory(p.Directory, path, info) {
			return ProjectMappingPreview{}, ErrConflict
		}
		if p.ID == id {
			preview.prior = p
		}
	}
	if id != "" && preview.prior.ID == "" {
		return ProjectMappingPreview{}, ErrNotFound
	}
	return preview, nil
}
func sameProjectDirectory(existing, path string, info os.FileInfo) bool {
	if existing == path {
		return true
	}
	other, err := os.Stat(existing)
	return err == nil && info != nil && os.SameFile(info, other)
}
func (r *ProjectRegistry) Register(ctx context.Context, path string, confirmed bool) (ProjectIdentity, error) {
	if !confirmed {
		return ProjectIdentity{}, ErrNotConfirmed
	}
	preview, err := r.PreviewRegister(ctx, path)
	if err != nil {
		return ProjectIdentity{}, err
	}
	return r.CommitMapping(ctx, preview, true)
}
func (r *ProjectRegistry) Relocate(ctx context.Context, id, path string, confirmed bool) (ProjectIdentity, error) {
	if !confirmed {
		return ProjectIdentity{}, ErrNotConfirmed
	}
	preview, err := r.PreviewRelocate(ctx, id, path)
	if err != nil {
		return ProjectIdentity{}, err
	}
	return r.CommitMapping(ctx, preview, true)
}
func (r *ProjectRegistry) CommitMapping(ctx context.Context, preview ProjectMappingPreview, confirmed bool) (ProjectIdentity, error) {
	if !confirmed || preview.registry != r || preview.identity == nil {
		return ProjectIdentity{}, ErrNotConfirmed
	}
	if err := operationContext(ctx); err != nil {
		return ProjectIdentity{}, err
	}
	path, err := projectDirectory(preview.plan.Directory)
	if err != nil || path != preview.plan.Directory {
		return ProjectIdentity{}, ErrConflict
	}
	info, err := os.Stat(path)
	if err != nil || !os.SameFile(info, preview.identity) {
		return ProjectIdentity{}, ErrConflict
	}
	dir, err := r.directory()
	if err != nil {
		return ProjectIdentity{}, err
	}
	defer dir.Close()
	unlock, err := lockStore(ctx, dir)
	if err != nil {
		return ProjectIdentity{}, SafeError(err)
	}
	defer unlock()
	projects, err := r.load(dir)
	if err != nil {
		return ProjectIdentity{}, err
	}
	id := preview.plan.ID
	index := -1
	for i, p := range projects {
		if sameProjectDirectory(p.Directory, path, info) {
			return ProjectIdentity{}, ErrConflict
		}
		if p.ID == id {
			if p != preview.prior {
				return ProjectIdentity{}, ErrConflict
			}
			index = i
		}
	}
	if id == "" {
		generated, err := neutralID()
		if err != nil {
			return ProjectIdentity{}, ErrNotCommitted
		}
		id = "project-" + string(generated)
		projects = append(projects, ProjectIdentity{ID: id, Directory: path})
		index = len(projects) - 1
	} else {
		if index < 0 {
			return ProjectIdentity{}, ErrConflict
		}
		projects[index].Directory = path
	}
	data, err := json.Marshal(projects)
	if err != nil {
		return ProjectIdentity{}, ErrNotCommitted
	}
	if err = replaceStoreFile(ctx, dir, r.name, data, nil); err != nil {
		return ProjectIdentity{}, SafeError(err)
	}
	return projects[index], nil
}
