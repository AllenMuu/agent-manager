package webconsole

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

const maxJSONBodyBytes = 64 << 10

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/projects/inspect" {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Register a project with POST.")
			return
		}
		var request struct {
			Path string `json:"path"`
		}
		if !decodeJSONBody(w, r, &request) {
			return
		}
		project, err := s.registerProject(request.Path)
		if err != nil {
			switch {
			case errors.Is(err, errProjectAlreadyRegistered):
				writeAPIError(w, http.StatusConflict, "project_already_registered", err.Error())
			case errors.Is(err, errRegisteredProjectChanged):
				writeAPIError(w, http.StatusConflict, "stale_project", "The registered project directory changed. Restart the console to register it again.")
			default:
				writeAPIError(w, http.StatusBadRequest, "invalid_project", err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Project projectDTO `json:"project"`
		}{Project: projectDTO{ID: project.ID, Name: project.Name, Path: project.Path}})
		return
	}
	if s.handleOperationAPI(w, r) {
		return
	}
	if s.handleReadAPI(w, r) {
		return
	}
	writeAPIError(w, http.StatusNotFound, "not_found", "The API route was not found.")
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, destination any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIError(w, http.StatusBadRequest, "invalid_content_type", "The request must use application/json.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "The JSON request exceeds the 64 KiB limit.")
		} else {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", "The request body must contain one valid JSON object with only supported fields.")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "The request body must contain exactly one JSON value.")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
