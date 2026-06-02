package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/TheDurtch/anime-request-server/internal/middleware"
	"github.com/TheDurtch/anime-request-server/internal/models"
	"github.com/TheDurtch/anime-request-server/internal/repository"
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

	// Check for duplicates
	dup, err := h.requests.CheckDuplicate(r.Context(), req.Name)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if dup {
		Error(w, http.StatusConflict, "a request with this name already exists")
		return
	}

	request, err := h.requests.Create(r.Context(), req.Name, models.Category(req.Category), user.ID)
	if err != nil {
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
		Status              *string `json:"status"`
		Category            *string `json:"category"`
		ServerDestinationIDs []string `json:"server_destination_ids"` // TODO: Implement full multi-destination UI
		AnidbURL            *string `json:"anidb_url"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
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

	// TODO: Handle req.ServerDestinationIDs via AddDestination/RemoveDestination methods

	// Handle AniDB URL - prevent clearing once set
	var anidbURL *string
	if req.AnidbURL != nil {
		// Check if request exists and has AniDB URL already set
		existing, err := h.requests.GetByID(r.Context(), id)
		if err != nil {
			Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		if existing == nil {
			Error(w, http.StatusNotFound, "request not found")
			return
		}

		// If trying to clear an existing AniDB URL, set it to "none" instead
		if existing.AnidbURL != nil && *req.AnidbURL == "" {
			none := "none"
			anidbURL = &none
		} else {
			anidbURL = req.AnidbURL
		}
	}

	if err := h.requests.Update(r.Context(), id, status, category, anidbURL); err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	updated, err := h.requests.GetByID(r.Context(), id)
	if err != nil || updated == nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusOK, updated)
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

// intQuery parses an integer query parameter with a default value.
func intQuery(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	var i int
	if _, err := parsePositiveInt(v); err != nil {
		return defaultVal
	}
	i = int(mustParsePositiveInt(v))
	return i
}

func parsePositiveInt(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	var i int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid integer: %s", s)
		}
		i = i*10 + int(c-'0')
	}
	return i, nil
}

func mustParsePositiveInt(s string) int {
	i, _ := parsePositiveInt(s)
	return i
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

	// Check if this is the last destination
	count, err := h.requests.GetDestinationCount(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	if count <= 1 {
		Error(w, http.StatusBadRequest, "cannot remove last destination")
		return
	}

	if err := h.requests.RemoveDestination(r.Context(), id, destID); err != nil {
		Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
