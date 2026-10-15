package skillsync

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxSkillLockBytes = 4 << 20

type installLock struct {
	Version int                        `json:"version"`
	Skills  map[string]json.RawMessage `json:"skills"`
}

type installRecord struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl"`
	Ref             string `json:"ref"`
	SkillPath       string `json:"skillPath"`
	SkillFolderHash string `json:"skillFolderHash"`
	ComputedHash    string `json:"computedHash"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
}

func attachSourceProvenance(sources []Source, home, project string) {
	globalPath := globalSkillLockPath(home)
	global := readInstallLock(globalPath)
	projectRecords := map[string]installRecord{}
	if project != "" {
		projectRecords = readInstallLock(filepath.Join(project, "skills-lock.json"))
	}
	for i := range sources {
		source := &sources[i]
		// Skills CLI lock files describe the default .agents/skills install
		// location. Do not attribute the same identifier in another adapter's
		// folder to this upstream record.
		if source.Location != ".agents" {
			source.Origin = source.Location
			source.OriginType = "local"
			continue
		}
		var record installRecord
		var found bool
		if source.Scope == ScopeGlobal {
			record, found = global[source.Identifier]
		} else {
			record, found = projectRecords[source.Identifier]
		}
		if !found {
			source.Origin = source.Location
			source.OriginType = "local"
			continue
		}
		if source.Scope == ScopeGlobal {
			source.LockPath = globalPath
		} else {
			source.LockPath = filepath.Join(project, "skills-lock.json")
		}
		source.LockKey = source.Identifier
		source.Origin = strings.TrimSpace(record.Source)
		source.OriginType = strings.ToLower(strings.TrimSpace(record.SourceType))
		if source.OriginType == "" {
			source.OriginType = "unknown"
		}
		if source.Origin == "" {
			source.Origin = source.Location
		}
		source.OriginRef = record.Ref
		source.OriginSkillPath = record.SkillPath
		source.InstalledAt = parseLockTime(record.InstalledAt)
		source.UpdatedAt = parseLockTime(record.UpdatedAt)
		if record.SkillFolderHash != "" {
			source.StoredHash = strings.ToLower(record.SkillFolderHash)
			source.HashKind = "git-tree"
		} else if record.ComputedHash != "" {
			source.StoredHash = strings.ToLower(record.ComputedHash)
			source.HashKind = "content"
		}
	}
}

func globalSkillLockPath(home string) string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); filepath.IsAbs(xdg) {
		if canonical, err := filepath.EvalSymlinks(xdg); err == nil {
			xdg = canonical
		}
		return filepath.Join(xdg, "skills", ".skill-lock.json")
	}
	return filepath.Join(home, ".agents", ".skill-lock.json")
}

// GlobalSkillLockPath returns the supported XDG-aware global Skills CLI lock path.
func GlobalSkillLockPath(home string) string { return globalSkillLockPath(home) }

func readInstallLock(path string) map[string]installRecord {
	result := map[string]installRecord{}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSkillLockBytes {
		return result
	}
	file, err := os.Open(path)
	if err != nil {
		return result
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxSkillLockBytes+1))
	if err != nil || len(contents) > maxSkillLockBytes {
		return result
	}
	var lock installLock
	if json.Unmarshal(contents, &lock) != nil || lock.Version < 1 || lock.Skills == nil {
		return result
	}
	for name, raw := range lock.Skills {
		if name == "" || len(raw) == 0 {
			continue
		}
		var record installRecord
		if json.Unmarshal(raw, &record) == nil {
			result[name] = record
		}
	}
	return result
}

func parseLockTime(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.IsZero() {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

func newestRegularFileMTime(root string) (time.Time, error) {
	var newest time.Time
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link %s is not eligible", path)
		}
		if info.Mode().IsRegular() && info.ModTime().After(newest) {
			newest = info.ModTime().UTC()
		}
		return nil
	})
	return newest, err
}
