package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Kagejitsu/anime-request-server/internal/middleware"
	"github.com/Kagejitsu/anime-request-server/internal/models"
	"github.com/Kagejitsu/anime-request-server/internal/repository"
)

// RequestHandler handles anime request endpoints.
type RequestHandler struct {
	requests *repository.RequestRepo
}

// NewRequestHandler creates a new RequestHandler.
func NewRequestHandler(requests *repository.RequestRepo) *RequestHandler {
	return &RequestHandler{requests: requests}
}

// List handles GET /api/v1/requests
func (h *RequestHandler) List(w http.ResponseWriter, r *http.Request) {
	filter := repository.RequestFilter{
		Page:    intQuery(r, "page", 1),
		PerPage: intQuery(r, "per_page", 25),
	}

	if c := r.URL.Query().Get("category"); c != "" {
		filter.Category = &c
	}
	if s := r.URL.Query().Get("status"); s != "" {
		filter.Status = &s
	}
	if rb := r.URL.Query().Get("requested_by"); rb != "" {
		if id, err := uuid.Parse(rb); err == nil {
			filter.RequestedBy = &id
		}
	}

	requests, total, err := h.requests.List(r.Context(), filter)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	if requests == nil {
		requests = []models.AnimeRequest{}
	}

	JSON(w, http.StatusOK, map[string]any{
		"requests": requests,
		"total":    total,
		"page":     filter.Page,
		"per_page": filter.PerPage,
	})
}

// Create handles POST /api/v1/requests
func (h *RequestHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	var req struct {
		Name     string `json:"name"`
		Category string `json:"category"`
		// Mod/admin-only optional fields, applied only when the caller is a
		// mod or admin; ignored for regular users.
		AltName              *string  `json:"alt_name"`
		Status               *string  `json:"status"`
		AnidbURL             *string  `json:"anidb_url"`
		ServerDestinationIDs []string `json:"server_destination_ids"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		Error(w, http.StatusBadRequest, "name is required")
		return
	}

	// Users can only create with current_future or finished_airing
	if req.Category != string(models.CategoryCurrentFuture) && req.Category != string(models.CategoryFinishedAiring) {
		Error(w, http.StatusBadRequest, "category must be 'current_future' or 'finished_airing'")
		return
	}

	var altName *string
	var status *models.Status
	var anidbURL *string
	var destIDs []uuid.UUID
	if user.IsModOrAdmin() {
		if req.AltName != nil {
			if trimmed := strings.TrimSpace(*req.AltName); trimmed != "" {
				if strings.EqualFold(trimmed, req.Name) {
					Error(w, http.StatusBadRequest, "alternate name must differ from the name")
					return
				}
				altName = &trimmed
			}
		}
		if req.Status != nil {
			if !models.IsValidStatus(*req.Status) {
				Error(w, http.StatusBadRequest, "invalid status")
				return
			}
			s := models.Status(*req.Status)
			status = &s
		}
		if req.AnidbURL != nil && *req.AnidbURL != "" {
			if !isValidHTTPURL(*req.AnidbURL) {
				Error(w, http.StatusBadRequest, "anidb_url must be a valid http(s) URL")
				return
			}
			anidbURL = req.AnidbURL
		}
		for _, idStr := range req.ServerDestinationIDs {
			id, err := uuid.Parse(idStr)
			if err != nil {
				Error(w, http.StatusBadRequest, "invalid server_destination_ids")
				return
			}
			destIDs = append(destIDs, id)
		}
	}

	// Check for duplicates against both the name and the alt name.
	candidates := []string{req.Name}
	if altName != nil {
		candidates = append(candidates, *altName)
	}
	dup, err := h.requests.NameInUse(r.Context(), uuid.Nil, candidates...)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if dup {
		Error(w, http.StatusConflict, "a request with this name already exists")
		return
	}

	request, err := h.requests.CreateWithDetails(r.Context(), req.Name, models.Category(req.Category), user.ID, altName, status, anidbURL, destIDs)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			Error(w, http.StatusConflict, "a request with this name already exists")
			return
		}
		if strings.Contains(err.Error(), "must differ") {
			Error(w, http.StatusBadRequest, "alternate name must differ from the name")
			return
		}
		if strings.Contains(err.Error(), "destination does not exist") {
			Error(w, http.StatusBadRequest, "invalid server_destination_ids")
			return
		}
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusCreated, request)
}

// Get handles GET /api/v1/requests/{id}
func (h *RequestHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	request, err := h.requests.GetByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if request == nil {
		Error(w, http.StatusNotFound, "request not found")
		return
	}

	JSON(w, http.StatusOK, request)
}

// Update handles PATCH /api/v1/requests/{id}
func (h *RequestHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	var req struct {
		Name     *string `json:"name"`
		AltName  *string `json:"alt_name"`
		Status   *string `json:"status"`
		Category *string `json:"category"`
		// Note: Destinations are managed via /requests/{id}/destinations endpoints
		AnidbURL *string `json:"anidb_url"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var name *string
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			Error(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		name = &trimmed
	}

	// alt_name: omitted = leave unchanged; "" = clear; otherwise set. The
	// repository treats a non-nil empty string as a clear (NULL).
	var altName *string
	if req.AltName != nil {
		trimmed := strings.TrimSpace(*req.AltName)
		if trimmed != "" && name != nil && strings.EqualFold(trimmed, *name) {
			Error(w, http.StatusBadRequest, "alternate name must differ from the name")
			return
		}
		altName = &trimmed
	}

	var status *models.Status
	if req.Status != nil {
		if !models.IsValidStatus(*req.Status) {
			Error(w, http.StatusBadRequest, "invalid status")
			return
		}
		s := models.Status(*req.Status)
		status = &s
	}

	var category *models.Category
	if req.Category != nil {
		if !models.IsValidCategory(*req.Category) {
			Error(w, http.StatusBadRequest, "invalid category")
			return
		}
		c := models.Category(*req.Category)
		category = &c
	}

	// Handle AniDB URL. A non-empty value must be a valid http(s) URL and
	// replaces the current one. An empty string intentionally leaves the
	// existing value unchanged rather than clearing it: every anime has an
	// AniDB entry, so a wrong URL is corrected by submitting the right one and
	// never needs to be cleared. This no-clear behavior is by design (and
	// replaces the old "none" sentinel) — not an oversight.
	var anidbURL *string
	if req.AnidbURL != nil && *req.AnidbURL != "" {
		if !isValidHTTPURL(*req.AnidbURL) {
			Error(w, http.StatusBadRequest, "anidb_url must be a valid http(s) URL")
			return
		}
		anidbURL = req.AnidbURL
	}

	// Catch cross-column collisions (new name/alt vs another request's name or
	// alt) the unique indexes can't, excluding this request itself.
	var candidates []string
	if name != nil {
		candidates = append(candidates, *name)
	}
	if altName != nil && *altName != "" {
		candidates = append(candidates, *altName)
	}
	if len(candidates) > 0 {
		dup, err := h.requests.NameInUse(r.Context(), id, candidates...)
		if err != nil {
			Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		if dup {
			Error(w, http.StatusConflict, "a request with this name already exists")
			return
		}
	}

	if err := h.requests.Update(r.Context(), id, name, altName, status, category, anidbURL); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			Error(w, http.StatusConflict, "a request with this name already exists")
			return
		}
		if strings.Contains(err.Error(), "must differ") {
			Error(w, http.StatusBadRequest, "alternate name must differ from the name")
			return
		}
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	updated, err := h.requests.GetByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if updated == nil {
		Error(w, http.StatusNotFound, "request not found")
		return
	}

	JSON(w, http.StatusOK, updated)
}

// Delete handles DELETE /api/v1/requests/{id}
func (h *RequestHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	deleted, err := h.requests.Delete(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		Error(w, http.StatusNotFound, "request not found")
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// BatchCreate handles POST /api/v1/requests/batch
func (h *RequestHandler) BatchCreate(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	if !user.CanBatchAdd {
		Error(w, http.StatusForbidden, "batch add permission required")
		return
	}

	var req struct {
		Names    []string `json:"names"`
		Category string   `json:"category"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Category != string(models.CategoryBatchAdd) {
		Error(w, http.StatusBadRequest, "batch add category must be 'batch_add'")
		return
	}

	// Trim and filter empty names
	var names []string
	for _, n := range req.Names {
		n = strings.TrimSpace(n)
		if n != "" {
			names = append(names, n)
		}
	}

	if len(names) == 0 {
		Error(w, http.StatusBadRequest, "at least one name is required")
		return
	}

	count, err := h.requests.CreateBatch(r.Context(), names, user.ID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusCreated, map[string]any{
		"added": count,
	})
}

// intQuery parses a positive integer query parameter, returning defaultVal when
// the parameter is absent, non-numeric, out of range, or less than 1.
func intQuery(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	// strconv.Atoi reports ErrRange on overflow, so this is overflow-safe.
	i, err := strconv.Atoi(v)
	if err != nil || i < 1 {
		return defaultVal
	}
	return i
}

// isValidHTTPURL reports whether s is a well-formed absolute http(s) URL.
func isValidHTTPURL(s string) bool {
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// AddDestination handles POST /api/v1/requests/{id}/destinations
func (h *RequestHandler) AddDestination(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	var req struct {
		DestinationID string `json:"destination_id"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	destID, err := uuid.Parse(req.DestinationID)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid destination_id")
		return
	}

	if err := h.requests.AddDestination(r.Context(), id, destID); err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "added"})
}

// RemoveDestination handles DELETE /api/v1/requests/{id}/destinations/{dest_id}
func (h *RequestHandler) RemoveDestination(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	destID, err := uuid.Parse(chi.URLParam(r, "dest_id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid destination ID")
		return
	}

	request, err := h.requests.GetByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if request == nil {
		Error(w, http.StatusNotFound, "request not found")
		return
	}

	// Verify the destination is actually assigned to this request.
	isMember := false
	for _, d := range request.ServerDestinations {
		if d.ID == destID {
			isMember = true
			break
		}
	}
	if !isMember {
		Error(w, http.StatusNotFound, "destination is not assigned to this request")
		return
	}

	// Preserve the "keep at least one destination" rule, but only when this is
	// genuinely the last assigned destination.
	if len(request.ServerDestinations) <= 1 {
		Error(w, http.StatusBadRequest, "cannot remove last destination")
		return
	}

	if err := h.requests.RemoveDestination(r.Context(), id, destID); err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// maxNoteLen caps a note body to keep posts reasonable.
const maxNoteLen = 2000

// ListNotes handles GET /api/v1/requests/{id}/notes (any authenticated user).
func (h *RequestHandler) ListNotes(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	notes, err := h.requests.ListNotes(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	JSON(w, http.StatusOK, notes)
}

// AddNote handles POST /api/v1/requests/{id}/notes. Any authenticated user may
// post unless they are blocked from posting notes.
func (h *RequestHandler) AddNote(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user.NotesBlocked {
		Error(w, http.StatusForbidden, "you are blocked from posting notes")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid request ID")
		return
	}

	var req struct {
		Body string `json:"body"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		Error(w, http.StatusBadRequest, "note body is required")
		return
	}
	if utf8.RuneCountInString(body) > maxNoteLen {
		Error(w, http.StatusBadRequest, "note is too long (max 2000 characters)")
		return
	}

	// Confirm the request exists so a bad ID is a clean 404, not a 500.
	existing, err := h.requests.GetByID(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existing == nil {
		Error(w, http.StatusNotFound, "request not found")
		return
	}

	note, err := h.requests.AddNote(r.Context(), id, user.ID, body)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	JSON(w, http.StatusCreated, note)
}

// DeleteNote handles DELETE /api/v1/requests/{id}/notes/{note_id} (admin/mod).
func (h *RequestHandler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid request ID")
		return
	}
	noteID, err := uuid.Parse(chi.URLParam(r, "note_id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid note ID")
		return
	}

	// Scoped by request ID: a note that isn't on this request is a 404.
	deleted, err := h.requests.DeleteNote(r.Context(), id, noteID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !deleted {
		Error(w, http.StatusNotFound, "note not found")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
