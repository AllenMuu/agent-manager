package webconsole

import (
	"errors"
	"net/http"
	"runtime"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/diagnostic"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/recommend"
	"github.com/AllenMuu/skill-manager/internal/search"
	"github.com/AllenMuu/skill-manager/internal/subagent"
)

type systemDTO struct {
	RuntimeVersion    string      `json:"runtimeVersion"`
	ProjectRegistered bool        `json:"projectRegistered"`
	Project           *projectDTO `json:"project,omitempty"`
}

type skillDTO struct {
	Identifier    string   `json:"identifier"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tags          []string `json:"tags"`
	Compatibility []string `json:"compatibility"`
	Provenance    string   `json:"provenance,omitempty"`
	Body          string   `json:"body,omitempty"`
}

type matchDTO struct {
	Technology string `json:"technology"`
	Field      string `json:"field"`
}

type recommendationDTO struct {
	Identifier       string     `json:"identifier"`
	Description      string     `json:"description"`
	MatchReason      string     `json:"matchReason"`
	MatchingEvidence []matchDTO `json:"matchingEvidence"`
	Confidence       string     `json:"confidence"`
	Score            int        `json:"score"`
}

type scopeDTO struct {
	Path            string              `json:"path"`
	Status          string              `json:"status"`
	Technologies    []technologyDTO     `json:"technologies"`
	Evidence        []evidenceDTO       `json:"evidence"`
	Diagnostics     []findingDTO        `json:"diagnostics"`
	Recommendations []recommendationDTO `json:"recommendations"`
	NextAction      string              `json:"nextAction"`
}

type technologyDTO struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Category string `json:"category"`
}

type evidenceDTO struct {
	Path       string `json:"path"`
	Technology string `json:"technology"`
}

type findingDTO struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type recommendationsDTO struct {
	Project      string       `json:"project"`
	ScanComplete bool         `json:"scanComplete"`
	Diagnostics  []findingDTO `json:"diagnostics"`
	Scopes       []scopeDTO   `json:"scopes"`
}

type latestOperationDTO struct {
	Operation     string   `json:"operation"`
	ResourceKind  string   `json:"resourceKind"`
	At            string   `json:"at"`
	UndoAvailable bool     `json:"undoAvailable"`
	UndoPreview   *planDTO `json:"undoPreview,omitempty"`
}

type planDTO struct {
	Version      string      `json:"version"`
	ResourceKind string      `json:"resourceKind"`
	Operation    string      `json:"operation"`
	Changes      []changeDTO `json:"changes"`
	Warnings     []string    `json:"warnings"`
}

type changeDTO struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

type subagentsDTO struct {
	Definitions []subagentDefinitionDTO `json:"definitions"`
	Diagnostics []subagent.Diagnostic   `json:"diagnostics"`
}

type subagentDefinitionDTO struct {
	Version              string                   `json:"version"`
	ID                   string                   `json:"id"`
	Name                 string                   `json:"name"`
	Role                 string                   `json:"role"`
	Instructions         string                   `json:"instructions"`
	Skills               []string                 `json:"skills"`
	Compatibility        subagentCompatibilityDTO `json:"compatibility"`
	RequiredCapabilities []string                 `json:"requiredCapabilities"`
}

type subagentCompatibilityDTO struct {
	Agents []string `json:"agents"`
}

func (s *Server) handleReadAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Read endpoints require GET.")
			return true
		}
		return false
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 3 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "system" {
		project := s.currentProject()
		if project != nil {
			if err := project.validate(); err != nil {
				writeAPIError(w, http.StatusConflict, "stale_project", "The registered project directory changed. Restart the console to register it again.")
				return true
			}
		}
		result := systemDTO{RuntimeVersion: runtime.Version(), ProjectRegistered: project != nil}
		if project != nil {
			view := viewProject(project)
			result.Project = &view
		}
		writeJSON(w, http.StatusOK, result)
		return true
	}
	if len(parts) == 3 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "skills" {
		s.handleSkillList(w, r)
		return true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "skills" {
		s.handleSkillDetail(w, parts[3])
		return true
	}
	if len(parts) == 3 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "subagents" {
		s.handleSubAgentList(w)
		return true
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "subagents" {
		s.handleSubAgentDetail(w, parts[3])
		return true
	}
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "projects" {
		return false
	}
	project := s.currentProject()
	if project == nil || parts[3] != project.ID {
		writeAPIError(w, http.StatusNotFound, "project_not_found", "The project is not registered in this console session.")
		return true
	}
	if err := project.validate(); err != nil {
		writeAPIError(w, http.StatusConflict, "stale_project", "The registered project directory changed. Restart the console to register it again.")
		return true
	}
	if len(parts) == 4 {
		writeJSON(w, http.StatusOK, struct {
			Project projectDTO `json:"project"`
		}{Project: viewProject(project)})
		return true
	}
	if len(parts) != 5 && !(len(parts) == 6 && parts[4] == "operations" && parts[5] == "latest") {
		return false
	}
	switch parts[4] {
	case "agents":
		s.handleAgents(w, project)
	case "recommendations":
		s.handleRecommendations(w, project)
	case "resources":
		s.handleResources(w, project)
	case "diagnostics":
		s.handleDiagnostics(w, project)
	case "operations":
		if len(parts) != 6 || parts[5] != "latest" {
			return false
		}
		s.handleLatestOperation(w, project)
	default:
		return false
	}
	return true
}

func (s *Server) currentProject() *registeredProject {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.project == nil {
		return nil
	}
	copy := *s.project
	return &copy
}

func (s *Server) handleAgents(w http.ResponseWriter, project *registeredProject) {
	items, err := adapter.Inventory(s.homeDir, project.Path)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "inventory_failed", "Agent inventory could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Agents []adapter.AgentInventory `json:"agents"`
	}{Agents: items})
}

func (s *Server) handleRecommendations(w http.ResponseWriter, project *registeredProject) {
	result, err := recommend.Recommend(project.Path, s.libraryPath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "recommendations_failed", "Project evidence or the configured Skill catalog could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, toRecommendationsDTO(result))
}

func (s *Server) handleResources(w http.ResponseWriter, project *registeredProject) {
	journal, err := s.journalForProject(project)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "unsafe_journal_path", "The project operation journal path is not a safe local directory.")
		return
	}
	items, err := lifecycle.New(s.libraryPath, journal, nil).List(project.Path)
	if err != nil {
		if errors.Is(err, lifecycle.ErrUnsafePath) {
			writeAPIError(w, http.StatusConflict, "unsafe_journal_entry", "The project operation journal contains paths outside its registered safety boundary.")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "inventory_failed", "Managed-resource inventory could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Resources []lifecycle.Item `json:"resources"`
	}{Resources: items})
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, project *registeredProject) {
	journal, err := s.journalForProject(project)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "unsafe_journal_path", "The project operation journal path is not a safe local directory.")
		return
	}
	findings, err := diagnostic.Scan(s.libraryPath, project.Path, journal)
	if err != nil {
		if errors.Is(err, lifecycle.ErrUnsafePath) {
			writeAPIError(w, http.StatusConflict, "unsafe_journal_entry", "The project operation journal contains paths outside its registered safety boundary.")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "diagnostics_failed", "Read-only diagnostics could not be completed.")
		return
	}
	result := make([]findingDTO, 0, len(findings))
	for _, finding := range findings {
		result = append(result, findingDTO{Path: finding.Path, Message: finding.Message})
	}
	writeJSON(w, http.StatusOK, struct {
		Findings []findingDTO `json:"findings"`
	}{Findings: result})
}

func (s *Server) handleLatestOperation(w http.ResponseWriter, project *registeredProject) {
	journal, err := s.journalForProject(project)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "unsafe_journal_path", "The project operation journal path is not a safe local directory.")
		return
	}
	entry, found, err := journal.Latest()
	if err != nil {
		if errors.Is(err, lifecycle.ErrUnsafePath) {
			writeAPIError(w, http.StatusConflict, "unsafe_journal_entry", "The project operation journal contains paths outside its registered safety boundary.")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "journal_unreadable", "The project operation journal could not be read.")
		return
	}
	var latest *latestOperationDTO
	if found {
		undoAllowed := validateConsoleUndoEntry(project, s.libraryPath, entry) == nil
		available := false
		var availabilityErr error
		if undoAllowed {
			available, availabilityErr = journal.UndoAvailable()
		}
		latest = &latestOperationDTO{Operation: entry.Operation, ResourceKind: entry.ResourceKind, At: entry.At.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), UndoAvailable: undoAllowed && availabilityErr == nil && available}
		if latest.UndoAvailable {
			preview, err := journal.PreviewUndo()
			if err != nil {
				latest.UndoAvailable = false
			} else {
				converted := toPlanDTO(preview)
				latest.UndoPreview = &converted
			}
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Latest *latestOperationDTO `json:"latest"`
	}{Latest: latest})
}

func (s *Server) handleSkillList(w http.ResponseWriter, r *http.Request) {
	skills, _, err := catalog.Discover(s.libraryPath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "catalog_unreadable", "The configured Skill catalog could not be read.")
		return
	}
	skills = search.Match(r.URL.Query().Get("q"), skills)
	result := make([]skillDTO, 0, len(skills))
	for _, skill := range skills {
		result = append(result, toSkillDTO(skill, false))
	}
	writeJSON(w, http.StatusOK, struct {
		Skills []skillDTO `json:"skills"`
	}{Skills: result})
}

func (s *Server) handleSkillDetail(w http.ResponseWriter, identifier string) {
	if identifier == "" || strings.ContainsAny(identifier, `/\\`) || identifier == "." || identifier == ".." {
		writeAPIError(w, http.StatusNotFound, "skill_not_found", "No eligible Skill has that identifier.")
		return
	}
	skills, _, err := catalog.Discover(s.libraryPath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "catalog_unreadable", "The configured Skill catalog could not be read.")
		return
	}
	for _, skill := range skills {
		if skill.Identifier == identifier {
			writeJSON(w, http.StatusOK, struct {
				Skill skillDTO `json:"skill"`
			}{Skill: toSkillDTO(skill, true)})
			return
		}
	}
	writeAPIError(w, http.StatusNotFound, "skill_not_found", "No eligible Skill has that identifier.")
}

func (s *Server) handleSubAgentList(w http.ResponseWriter) {
	result, err := s.discoverSubAgents()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "subagents_unavailable", "Canonical SubAgent definitions could not be read.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSubAgentDetail(w http.ResponseWriter, identifier string) {
	if identifier == "" || strings.ContainsAny(identifier, `/\\`) || identifier == "." || identifier == ".." {
		writeAPIError(w, http.StatusNotFound, "subagent_not_found", "No valid canonical SubAgent has that identifier.")
		return
	}
	result, err := s.discoverSubAgents()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "subagents_unavailable", "Canonical SubAgent definitions could not be read.")
		return
	}
	for _, definition := range result.Definitions {
		if definition.ID == identifier {
			writeJSON(w, http.StatusOK, struct {
				Definition subagentDefinitionDTO `json:"definition"`
			}{Definition: definition})
			return
		}
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.ID == identifier {
			writeJSON(w, http.StatusOK, struct {
				Diagnostic subagent.Diagnostic `json:"diagnostic"`
			}{Diagnostic: diagnostic})
			return
		}
	}
	writeAPIError(w, http.StatusNotFound, "subagent_not_found", "No valid canonical SubAgent has that identifier.")
}

func (s *Server) discoverSubAgents() (subagentsDTO, error) {
	skills, _, err := catalog.Discover(s.libraryPath)
	if err != nil {
		return subagentsDTO{}, err
	}
	eligible := make(map[string]bool, len(skills))
	for _, skill := range skills {
		eligible[skill.Identifier] = true
	}
	registry, err := subagent.NewRegistry(s.dataRoot, func(identifier string) bool { return eligible[identifier] })
	if err != nil {
		return subagentsDTO{}, err
	}
	definitions, diagnostics, err := registry.Discover()
	if err != nil {
		return subagentsDTO{}, err
	}
	if definitions == nil {
		definitions = []subagent.Definition{}
	}
	if diagnostics == nil {
		diagnostics = []subagent.Diagnostic{}
	}
	converted := make([]subagentDefinitionDTO, 0, len(definitions))
	for _, definition := range definitions {
		converted = append(converted, toSubAgentDefinitionDTO(definition))
	}
	return subagentsDTO{Definitions: converted, Diagnostics: diagnostics}, nil
}

func toSubAgentDefinitionDTO(definition subagent.Definition) subagentDefinitionDTO {
	result := subagentDefinitionDTO{
		Version: definition.Version, ID: definition.ID, Name: definition.Name, Role: definition.Role,
		Instructions: definition.Instructions, Skills: append([]string{}, definition.Skills...),
		Compatibility:        subagentCompatibilityDTO{Agents: append([]string{}, definition.Compatibility.Agents...)},
		RequiredCapabilities: make([]string, 0, len(definition.RequiredCapabilities)),
	}
	for _, capability := range definition.RequiredCapabilities {
		result.RequiredCapabilities = append(result.RequiredCapabilities, string(capability))
	}
	return result
}

func toSkillDTO(skill catalog.Skill, includeBody bool) skillDTO {
	result := skillDTO{Identifier: skill.Identifier, Name: skill.Name, Description: skill.Description, Tags: skill.Tags, Compatibility: skill.Compatibility, Provenance: skill.Provenance}
	if result.Tags == nil {
		result.Tags = []string{}
	}
	if result.Compatibility == nil {
		result.Compatibility = []string{}
	}
	if includeBody {
		result.Body = skill.Body
	}
	return result
}

func toRecommendationsDTO(result recommend.Result) recommendationsDTO {
	out := recommendationsDTO{Project: result.Project, ScanComplete: result.ScanComplete, Diagnostics: make([]findingDTO, 0, len(result.Diagnostics)), Scopes: make([]scopeDTO, 0, len(result.Scopes))}
	for _, finding := range result.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, findingDTO{Path: finding.Path, Message: finding.Message})
	}
	for _, scope := range result.Scopes {
		converted := scopeDTO{Path: scope.Path, Status: string(scope.Status), NextAction: string(scope.NextAction), Technologies: make([]technologyDTO, 0, len(scope.Technologies)), Evidence: make([]evidenceDTO, 0, len(scope.Evidence)), Diagnostics: make([]findingDTO, 0, len(scope.Diagnostics)), Recommendations: make([]recommendationDTO, 0, len(scope.Recommendations))}
		for _, technology := range scope.Technologies {
			converted.Technologies = append(converted.Technologies, technologyDTO{ID: technology.ID, Label: technology.Label, Category: string(technology.Category)})
		}
		for _, evidence := range scope.Evidence {
			converted.Evidence = append(converted.Evidence, evidenceDTO{Path: evidence.Path, Technology: evidence.Technology})
		}
		for _, finding := range scope.Diagnostics {
			converted.Diagnostics = append(converted.Diagnostics, findingDTO{Path: finding.Path, Message: finding.Message})
		}
		for _, recommendation := range scope.Recommendations {
			item := recommendationDTO{Identifier: recommendation.Identifier, Description: recommendation.Description, MatchReason: recommendation.MatchReason, Confidence: string(recommendation.Confidence), Score: recommendation.Score, MatchingEvidence: make([]matchDTO, 0, len(recommendation.MatchingEvidence))}
			for _, evidence := range recommendation.MatchingEvidence {
				item.MatchingEvidence = append(item.MatchingEvidence, matchDTO{Technology: evidence.Technology, Field: evidence.Field})
			}
			converted.Recommendations = append(converted.Recommendations, item)
		}
		out.Scopes = append(out.Scopes, converted)
	}
	return out
}

func toPlanDTO(plan operation.Plan) planDTO {
	out := planDTO{Version: plan.Version, ResourceKind: plan.ResourceKind, Operation: plan.Operation, Changes: make([]changeDTO, 0, len(plan.Changes)), Warnings: plan.Warnings}
	for _, change := range plan.Changes {
		out.Changes = append(out.Changes, changeDTO{Path: change.Path, Action: change.Action, Detail: change.Detail})
	}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	return out
}
