package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	res, err := s.app.ListArtifacts(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleUploadArtifact(w http.ResponseWriter, r *http.Request) {
	// Bound request body to 2 GiB and spill >32 MiB uploads safely to temp disk
	r.Body = http.MaxBytesReader(w, r.Body, 2<<30)
	// #nosec G120 -- request body explicitly bounded to 2 GiB via http.MaxBytesReader above
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "ART", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "ART", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	defer func() { _ = file.Close() }()

	entry, err := s.app.UploadArtifact(
		r.Context(),
		ActorFromContext(r.Context()),
		chi.URLParam(r, "projectId"),
		r.FormValue("category"),
		r.FormValue("product"),
		r.FormValue("version"),
		r.FormValue("arch"),
		hdr.Filename,
		file,
	)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (s *Server) handlePrefetchArtifact(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL      string `json:"url"`
		Category string `json:"category"`
		Product  string `json:"product"`
		Version  string `json:"version"`
		Arch     string `json:"arch"`
		FileName string `json:"fileName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "ART", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	entry, err := s.app.PrefetchArtifact(
		r.Context(),
		ActorFromContext(r.Context()),
		chi.URLParam(r, "projectId"),
		body.URL, body.Category, body.Product, body.Version, body.Arch, body.FileName,
	)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	logicalPath := r.URL.Query().Get("path")
	if logicalPath == "" {
		var body struct {
			LogicalPath string `json:"logicalPath"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		logicalPath = body.LogicalPath
	}
	if err := s.app.DeleteArtifact(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), logicalPath); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": logicalPath})
}

func (s *Server) handleExportArtifactBOM(w http.ResponseWriter, r *http.Request) {
	m, err := s.app.ExportArtifactBOM(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"))
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="kinstall-bom-manifest.json"`)
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handlePushImageArtifact(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LogicalPath string `json:"logicalPath"`
		HostID      string `json:"hostId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "ART", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	res, err := s.app.PushImageArtifactToHost(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body.LogicalPath, body.HostID)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
