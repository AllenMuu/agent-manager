package stack

import (
	"encoding/json"
	"sort"
	"strings"
)

// maxMarkerBytes is the static read budget for one marker file.
const maxMarkerBytes = 1 << 20

// markerParser derives normalized technology identifiers from marker bytes.
// It never executes content and never uses versions as criteria.
type markerParser func(contents []byte) []string

// marker describes one documented static project marker.
type marker struct {
	name          string
	scopeBoundary bool
	parse         markerParser
}

// markers is the declarative registry of supported static markers.
var markers = []marker{
	{name: "go.mod", scopeBoundary: true, parse: simple("go")},
	{name: "package.json", scopeBoundary: true, parse: parsePackageJSON},
	{name: "tsconfig.json", scopeBoundary: true, parse: simple("typescript")},
	{name: "pom.xml", scopeBoundary: true, parse: mavenParser},
	{name: "build.gradle", scopeBoundary: true, parse: gradleParser},
	{name: "build.gradle.kts", scopeBoundary: true, parse: gradleParser},
	{name: "pyproject.toml", scopeBoundary: true, parse: pythonParser},
	{name: "Cargo.toml", scopeBoundary: true, parse: simple("rust")},
	{name: "Gemfile", scopeBoundary: true, parse: rubyParser},
	{name: "Dockerfile", scopeBoundary: false, parse: dockerParser},
	{name: "docker-compose.yml", scopeBoundary: false, parse: composeParser},
	{name: "docker-compose.yaml", scopeBoundary: false, parse: composeParser},
	{name: "compose.yml", scopeBoundary: false, parse: composeParser},
	{name: "compose.yaml", scopeBoundary: false, parse: composeParser},
	{name: ".claude", scopeBoundary: false, parse: simple("claude-code")},
	{name: ".codex", scopeBoundary: false, parse: simple("codex")},
	{name: ".agents", scopeBoundary: false, parse: simple("agents")},
}

// excludedDirectories are never traversed during scope discovery.
var excludedDirectories = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, ".cache": true, ".next": true,
	".turbo": true, ".venv": true, "venv": true, "__pycache__": true,
}

// markerByName returns the registered marker for a file or directory name.
func markerByName(name string) (marker, bool) {
	for _, m := range markers {
		if m.name == name {
			return m, true
		}
	}
	return marker{}, false
}

func simple(ids ...string) markerParser {
	return func([]byte) []string { return append([]string(nil), ids...) }
}

// dependencyKeywords maps lowercase text fragments to technologies for
// text-derived framework detection inside manifests.
var dependencyKeywords = []struct {
	fragment string
	id       string
}{
	{"spring-boot", "spring-boot"},
	{"django", "django"},
	{"fastapi", "fastapi"},
	{"rails", "rails"},
}

func parsePackageJSON(contents []byte) []string {
	var doc struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	ids := []string{"nodejs"}
	if err := json.Unmarshal(contents, &doc); err != nil {
		return ids
	}
	deps := map[string]bool{}
	for name := range doc.Dependencies {
		deps[name] = true
	}
	for name := range doc.DevDependencies {
		deps[name] = true
	}
	for name := range doc.OptionalDependencies {
		deps[name] = true
	}
	if deps["typescript"] {
		ids = append(ids, "typescript")
	}
	if deps["express"] {
		ids = append(ids, "express")
	}
	if deps["@nestjs/core"] {
		ids = append(ids, "nestjs")
	}
	if deps["react"] {
		ids = append(ids, "react")
	}
	if deps["next"] {
		ids = append(ids, "nextjs")
	}
	return ids
}

func mavenParser(contents []byte) []string {
	ids := []string{"maven"}
	for _, kw := range dependencyKeywords {
		if strings.Contains(strings.ToLower(string(contents)), kw.fragment) {
			ids = append(ids, kw.id)
		}
	}
	return ids
}

func gradleParser(contents []byte) []string {
	ids := []string{"gradle"}
	for _, kw := range dependencyKeywords {
		if strings.Contains(strings.ToLower(string(contents)), kw.fragment) {
			ids = append(ids, kw.id)
		}
	}
	return ids
}

func pythonParser(contents []byte) []string {
	ids := []string{"python"}
	for _, kw := range dependencyKeywords {
		if strings.Contains(strings.ToLower(string(contents)), kw.fragment) {
			ids = append(ids, kw.id)
		}
	}
	return ids
}

func rubyParser(contents []byte) []string {
	ids := []string{"ruby"}
	for _, kw := range dependencyKeywords {
		if strings.Contains(strings.ToLower(string(contents)), kw.fragment) {
			ids = append(ids, kw.id)
		}
	}
	return ids
}

// imageKeywords map container image fragments to database technologies.
var imageKeywords = []struct {
	fragment string
	id       string
}{
	{"postgres", "postgresql"},
	{"mysql", "mysql"},
	{"redis", "redis"},
	{"mongo", "mongodb"},
}

func dockerParser(contents []byte) []string {
	ids := []string{"docker"}
	lower := strings.ToLower(string(contents))
	for _, line := range strings.Split(lower, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "from ") {
			continue
		}
		for _, kw := range imageKeywords {
			if strings.Contains(trimmed, kw.fragment) {
				ids = append(ids, kw.id)
			}
		}
	}
	return dedupeSorted(ids)
}

func composeParser(contents []byte) []string {
	ids := []string{"compose", "docker"}
	lower := strings.ToLower(string(contents))
	for _, kw := range imageKeywords {
		if strings.Contains(lower, kw.fragment) {
			ids = append(ids, kw.id)
		}
	}
	return dedupeSorted(ids)
}

func dedupeSorted(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
