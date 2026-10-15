package skillsync

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func hashLocalSkill(root, kind string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Skill directory is not a real directory")
	}
	files := make([]remoteFile, 0)
	total := int64(0)
	err = filepath.WalkDir(root, func(pathname string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, pathname)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if kind == "content" && hasExcludedHashComponent(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Skill tree contains a symbolic link")
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxSkillFileBytes || total+info.Size() > maxSkillTotalBytes || len(files) >= maxSkillFiles {
			return errors.New("Skill tree contains an unsupported file or exceeds size limits")
		}
		file, err := os.Open(pathname)
		if err != nil {
			return err
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxSkillFileBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if int64(len(content)) != info.Size() || len(content) > maxSkillFileBytes || total+int64(len(content)) > maxSkillTotalBytes {
			return errors.New("Skill file changed or exceeds size limits")
		}
		mode := "100644"
		if info.Mode().Perm()&0o111 != 0 {
			mode = "100755"
		}
		files = append(files, remoteFile{Path: relative, Mode: mode, Content: content})
		total += int64(len(content))
		return nil
	})
	if err != nil {
		return "", err
	}
	if kind == "content" {
		return computeContentHash(files), nil
	}
	if kind == "git-tree" {
		hash := gitTreeHash(files)
		return hex.EncodeToString(hash[:]), nil
	}
	return "", errors.New("unsupported Skill lock hash type")
}

func localHashFileCount(root, kind string) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(pathname string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, pathname)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if kind == "content" && hasExcludedHashComponent(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Skill tree contains a symbolic link")
		}
		if info.Mode().IsRegular() {
			count++
			if count > maxSkillFiles {
				return errors.New("Skill tree exceeds supported file count")
			}
		}
		return nil
	})
	return count, err
}

func fingerprintRegularFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxSkillLockBytes {
		return "", errors.New("lock file is not a regular bounded file")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if int64(len(contents)) != info.Size() {
		return "", errors.New("lock file changed while being read")
	}
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:]), nil
}

func lockContentHash(files []remoteFile) string {
	filtered := make([]remoteFile, 0, len(files))
	for _, file := range files {
		if !hasExcludedHashComponent(file.Path) {
			filtered = append(filtered, file)
		}
	}
	return computeContentHash(filtered)
}

func normalizeSkillPath(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), "/")
}
