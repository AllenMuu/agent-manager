// Package catalog preserves the Skill catalog API while resource handlers own
// catalog discovery and transformation.
package catalog

import "github.com/AllenMuu/skill-manager/internal/resource"

// Skill is an eligible directory skill discovered in a skill library.
type Skill = resource.SkillCatalogEntry

// Diagnostic explains why a library entry is not eligible for management.
type Diagnostic = resource.Diagnostic

// Discover reads immediate child directories of root that contain valid SKILL.md
// files. It only reads metadata files; it never executes skill-provided content.
func Discover(root string) ([]Skill, []Diagnostic, error) {
	return resource.NewSkillHandler().Discover(root)
}
