// Package stack detects a project's technology stack from static markers.
package stack

// Category classifies a technology in the normalized vocabulary.
type Category string

const (
	CategoryLanguage  Category = "language"
	CategoryFramework Category = "framework"
	CategoryBuildTool Category = "build-tool"
	CategoryDatabase  Category = "database"
	CategoryContainer Category = "container"
	CategoryAgent     Category = "agent"
)

// Technology is one entry in the normalized technology vocabulary. ID is the
// canonical identifier used as the join point between markers and tags.
type Technology struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Category Category `json:"category"`
}

// vocabulary is the initial documented technology vocabulary.
var vocabulary = []Technology{
	{ID: "go", Label: "Go", Category: CategoryLanguage},
	{ID: "nodejs", Label: "Node.js", Category: CategoryLanguage},
	{ID: "typescript", Label: "TypeScript", Category: CategoryLanguage},
	{ID: "maven", Label: "Maven", Category: CategoryBuildTool},
	{ID: "gradle", Label: "Gradle", Category: CategoryBuildTool},
	{ID: "python", Label: "Python", Category: CategoryLanguage},
	{ID: "rust", Label: "Rust", Category: CategoryLanguage},
	{ID: "ruby", Label: "Ruby", Category: CategoryLanguage},
	{ID: "docker", Label: "Docker", Category: CategoryContainer},
	{ID: "compose", Label: "Compose", Category: CategoryContainer},
	{ID: "claude-code", Label: "Claude Code", Category: CategoryAgent},
	{ID: "codex", Label: "Codex", Category: CategoryAgent},
	{ID: "agents", Label: "Agents", Category: CategoryAgent},
	{ID: "express", Label: "Express", Category: CategoryFramework},
	{ID: "nestjs", Label: "NestJS", Category: CategoryFramework},
	{ID: "react", Label: "React", Category: CategoryFramework},
	{ID: "nextjs", Label: "Next.js", Category: CategoryFramework},
	{ID: "spring-boot", Label: "Spring Boot", Category: CategoryFramework},
	{ID: "django", Label: "Django", Category: CategoryFramework},
	{ID: "fastapi", Label: "FastAPI", Category: CategoryFramework},
	{ID: "rails", Label: "Rails", Category: CategoryFramework},
	{ID: "postgresql", Label: "PostgreSQL", Category: CategoryDatabase},
	{ID: "mysql", Label: "MySQL", Category: CategoryDatabase},
	{ID: "redis", Label: "Redis", Category: CategoryDatabase},
	{ID: "mongodb", Label: "MongoDB", Category: CategoryDatabase},
}

var byID = func() map[string]Technology {
	m := make(map[string]Technology, len(vocabulary))
	for _, t := range vocabulary {
		m[t.ID] = t
	}
	return m
}()

// Vocabulary returns the full documented technology vocabulary.
func Vocabulary() []Technology {
	out := make([]Technology, len(vocabulary))
	copy(out, vocabulary)
	return out
}

// Lookup returns the vocabulary entry for a canonical identifier.
func Lookup(id string) (Technology, bool) {
	t, ok := byID[id]
	return t, ok
}
