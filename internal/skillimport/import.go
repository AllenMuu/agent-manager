// Package skillimport previews and copies explicitly selected Skills from a
// legacy flat library into the versioned Agent Manager repository.
package skillimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
	"gopkg.in/yaml.v3"
)

var (
	ErrInvalidSelection = errors.New("invalid Skill import selection")
	ErrConflict         = errors.New("Skill import has destination conflicts")
	ErrStalePreview     = errors.New("Skill import preview changed; review a new preview")
)

// Entry is one legacy Skill and its proposed repository destination.
type Entry struct {
	Identifier   string   `json:"identifier"`
	Source       string   `json:"source"`
	Destination  string   `json:"destination"`
	VersionKey   string   `json:"versionKey,omitempty"`
	Checksum     string   `json:"checksum,omitempty"`
	MetadataGaps []string `json:"metadataGaps,omitempty"`
	Eligible     bool     `json:"eligible"`
	Conflict     bool     `json:"conflict"`
	Problem      string   `json:"problem,omitempty"`
}

// Preview is a read-only, deterministic snapshot of a legacy import request.
type Preview struct {
	SourceRoot        string               `json:"sourceRoot"`
	DestinationRoot   string               `json:"destinationRoot"`
	SelectedIDs       []string             `json:"selectedIds,omitempty"`
	Entries           []Entry              `json:"entries"`
	Diagnostics       []catalog.Diagnostic `json:"diagnostics"`
	ConfirmationToken string               `json:"confirmationToken"`
}

// Service owns the old flat source and the new versioned destination roots.
type Service struct {
	repository *skillrepo.Repository
	sourceRoot string
}

// New constructs a migration service without creating either library.
func New(repositoryRoot, legacyRoot string) (*Service, error) {
	repository, err := skillrepo.Open(repositoryRoot)
	if err != nil {
		return nil, err
	}
	absLegacy, err := filepath.Abs(legacyRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve legacy Skill library: %w", err)
	}
	overlaps, err := skillrepo.PathsOverlap(repository.Root(), absLegacy)
	if err != nil {
		return nil, fmt.Errorf("resolve Skill library boundaries: %w", err)
	}
	if overlaps {
		return nil, fmt.Errorf("legacy and destination Skill libraries must not overlap")
	}
	return &Service{repository: repository, sourceRoot: filepath.Clean(absLegacy)}, nil
}

// Preview reports all eligible Skills when identifiers is empty. Supplying
// identifiers narrows the plan to that explicit selection without writing.
func (s *Service) Preview(identifiers []string) (Preview, error) {
	rootInfo, err := os.Lstat(s.sourceRoot)
	if err != nil {
		return Preview{}, fmt.Errorf("inspect legacy Skill library: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return Preview{}, fmt.Errorf("legacy Skill library is not a real directory")
	}
	if err := validateRepositoryRoot(s.repository.Root()); err != nil {
		return Preview{}, err
	}
	skills, diagnostics, err := catalog.Discover(s.sourceRoot)
	if err != nil {
		return Preview{}, err
	}
	byID := make(map[string]catalog.Skill, len(skills))
	for _, skill := range skills {
		byID[skill.Identifier] = skill
	}
	selected, err := normalizeSelection(identifiers, byID)
	if err != nil {
		return Preview{}, err
	}
	if len(selected) == 0 {
		for identifier := range byID {
			selected = append(selected, identifier)
		}
		sort.Strings(selected)
	}
	var selectedIDs []string
	if len(identifiers) > 0 {
		selectedIDs = append([]string(nil), selected...)
	}
	preview := Preview{
		SourceRoot: s.sourceRoot, DestinationRoot: s.repository.Root(),
		SelectedIDs: selectedIDs, Entries: make([]Entry, 0, len(selected)),
		Diagnostics: diagnostics,
	}
	for _, identifier := range selected {
		skill := byID[identifier]
		entry := Entry{Identifier: identifier, Source: skill.SourcePath, Eligible: true}
		checksum, checksumErr := skillrepo.ChecksumTree(skill.SourcePath)
		if checksumErr != nil {
			entry.Eligible = false
			entry.Problem = checksumErr.Error()
		} else {
			entry.Checksum = checksum
			entry.VersionKey = "legacy-" + checksum[:12]
		}
		entry.Destination = filepath.Join(s.repository.Root(), identifier, "versions", entry.VersionKey)
		entry.MetadataGaps = metadataGaps(skill.SourcePath)
		destination := filepath.Join(s.repository.Root(), identifier)
		if _, statErr := os.Lstat(destination); statErr == nil {
			entry.Conflict = true
		} else if !os.IsNotExist(statErr) {
			entry.Eligible = false
			entry.Problem = fmt.Sprintf("inspect destination: %v", statErr)
		}
		preview.Entries = append(preview.Entries, entry)
	}
	token, err := confirmationToken(preview)
	if err != nil {
		return Preview{}, fmt.Errorf("create Skill import confirmation token: %w", err)
	}
	preview.ConfirmationToken = token
	return preview, nil
}

// Import revalidates the exact displayed batch, then copies only its selected
// Skills. Conflicts refuse the entire batch; repository writes roll back if a
// later selected Skill fails.
func (s *Service) Import(preview Preview) ([]skillrepo.Entry, error) {
	if len(preview.SelectedIDs) == 0 {
		return nil, fmt.Errorf("%w: select one or more Skill identifiers", ErrInvalidSelection)
	}
	expectedToken, err := confirmationToken(preview)
	if err != nil || preview.ConfirmationToken == "" || preview.ConfirmationToken != expectedToken {
		return nil, ErrStalePreview
	}
	if filepath.Clean(preview.SourceRoot) != s.sourceRoot || filepath.Clean(preview.DestinationRoot) != s.repository.Root() {
		return nil, ErrStalePreview
	}
	fresh, err := s.Preview(preview.SelectedIDs)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(preview, fresh) {
		return nil, ErrStalePreview
	}
	inputs := make([]skillrepo.VersionSource, 0, len(fresh.Entries))
	for _, entry := range fresh.Entries {
		if !entry.Eligible {
			return nil, fmt.Errorf("%w: %s: %s", ErrInvalidSelection, entry.Identifier, entry.Problem)
		}
		if entry.Conflict {
			return nil, fmt.Errorf("%w: %s", ErrConflict, entry.Identifier)
		}
		inputs = append(inputs, skillrepo.VersionSource{
			Identifier: entry.Identifier,
			SourcePath: entry.Source,
			Metadata: skillrepo.VersionMetadata{
				Key: entry.VersionKey, Source: entry.Source, SourceType: "local-import",
				Version: "unknown", Publisher: "unknown", ImmutableRef: "unknown",
				Checksum: entry.Checksum, TrustLevel: "unknown", VerificationStatus: "unknown",
			},
			MakeCurrent: true,
		})
	}
	return s.repository.AddVersionsBatch(inputs)
}

func confirmationToken(preview Preview) (string, error) {
	preview.ConfirmationToken = ""
	contents, err := json.Marshal(preview)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:]), nil
}

func normalizeSelection(identifiers []string, byID map[string]catalog.Skill) ([]string, error) {
	if len(identifiers) == 0 {
		return nil, nil
	}
	selected := append([]string(nil), identifiers...)
	seen := make(map[string]struct{}, len(selected))
	for _, identifier := range selected {
		if strings.TrimSpace(identifier) != identifier || identifier == "" {
			return nil, fmt.Errorf("%w: invalid identifier %q", ErrInvalidSelection, identifier)
		}
		if _, exists := seen[identifier]; exists {
			return nil, fmt.Errorf("%w: duplicate identifier %q", ErrInvalidSelection, identifier)
		}
		seen[identifier] = struct{}{}
		if _, exists := byID[identifier]; !exists {
			return nil, fmt.Errorf("%w: Skill %q is not eligible in the legacy library", ErrInvalidSelection, identifier)
		}
	}
	sort.Strings(selected)
	return selected, nil
}

func metadataGaps(skillPath string) []string {
	gaps := []string{"declared version", "publisher", "immutable revision", "trust verification"}
	contents, err := os.ReadFile(filepath.Join(skillPath, ".skill-manager.yaml"))
	if err != nil {
		return append(gaps, "legacy companion metadata")
	}
	var metadata struct {
		Tags                []string `yaml:"tags"`
		Compatibility       []string `yaml:"compatibility"`
		SupportedLanguages  []string `yaml:"supportedLanguages"`
		SupportedFrameworks []string `yaml:"supportedFrameworks"`
		SupportedAgents     []string `yaml:"supportedAgents"`
		Provenance          string   `yaml:"provenance"`
	}
	if yaml.Unmarshal(contents, &metadata) != nil {
		return append(gaps, "valid legacy companion metadata")
	}
	if len(metadata.Tags) == 0 {
		gaps = append(gaps, "tags")
	}
	if len(metadata.Compatibility) == 0 {
		gaps = append(gaps, "compatibility")
	}
	if len(metadata.SupportedLanguages) == 0 {
		gaps = append(gaps, "supported languages")
	}
	if len(metadata.SupportedFrameworks) == 0 {
		gaps = append(gaps, "supported frameworks")
	}
	if len(metadata.SupportedAgents) == 0 {
		gaps = append(gaps, "supported agents")
	}
	if metadata.Provenance == "" {
		gaps = append(gaps, "legacy provenance")
	}
	return gaps
}

func validateRepositoryRoot(root string) error {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect destination Skill library: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("destination Skill library is not a real directory")
	}
	return nil
}
