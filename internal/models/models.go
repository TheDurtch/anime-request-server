package models

import (
	"time"

	"github.com/google/uuid"
)

// Role represents a user's permission level.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleMod   Role = "mod"
	RoleUser  Role = "user"
)

// User represents a registered user.
type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	Email        *string   `json:"email,omitempty"`
	PasswordHash string    `json:"-"`
	TOTPSecret   *string   `json:"-"`
	TOTPEnabled  bool      `json:"totp_enabled"`
	Role         Role      `json:"role"`
	CanBatchAdd  bool      `json:"can_batch_add"`
	Disabled     bool      `json:"disabled"`
	NotesBlocked bool      `json:"notes_blocked"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// IsAdmin returns true if the user is an admin.
func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

// IsModOrAdmin returns true if the user is a mod or admin.
func (u *User) IsModOrAdmin() bool {
	return u.Role == RoleAdmin || u.Role == RoleMod
}

func (u *User) IsMod() bool {
	return u.Role == RoleMod
}

func (u *User) IsUser() bool {
	return u.Role == RoleUser
}

// Category represents the anime request category.
type Category string

const (
	CategoryCurrentFuture  Category = "current_future"
	CategoryFinishedAiring Category = "finished_airing"
	CategoryBatchAdd       Category = "batch_add"
)

// Status represents the current state of a request.
type Status string

const (
	StatusNew        Status = "new"
	StatusDone       Status = "done"
	StatusNeedToGet  Status = "need_to_get"
	StatusAcquiring  Status = "acquiring"
	StatusProcessing Status = "processing"
	StatusSyncing    Status = "syncing"
)

// AnimeRequest represents a single anime request entry.
type AnimeRequest struct {
	ID                  uuid.UUID                  `json:"id"`
	Name                string                     `json:"name"`
	AltName             *string                    `json:"alt_name,omitempty"`
	Category            Category                   `json:"category"`
	Status              Status                     `json:"status"`
	RequestedBy         uuid.UUID                  `json:"requested_by"`
	RequestedByUsername string                     `json:"requested_by_username,omitempty"`
	ServerDestinations  []ServerDestinationMapping `json:"server_destinations,omitempty"`
	AnidbURL            *string                    `json:"anidb_url,omitempty"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
}

// Note is a single attributed, timestamped note on a request.
type Note struct {
	ID             uuid.UUID `json:"id"`
	RequestID      uuid.UUID `json:"request_id"`
	AuthorID       uuid.UUID `json:"author_id"`
	AuthorUsername string    `json:"author_username"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
}

// ServerDestinationMapping represents a server destination assigned to a request.
type ServerDestinationMapping struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	AddedAt time.Time `json:"added_at"`
}

// ServerDestination represents a managed server name.
type ServerDestination struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedBy uuid.UUID `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// InviteCode represents a signup invitation.
type InviteCode struct {
	ID        uuid.UUID  `json:"id"`
	Code      string     `json:"code"`
	CreatedBy uuid.UUID  `json:"created_by"`
	UsedBy    *uuid.UUID `json:"used_by,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Session represents an authenticated user session.
type Session struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ValidCategories returns all valid category values.
func ValidCategories() []Category {
	return []Category{CategoryCurrentFuture, CategoryFinishedAiring, CategoryBatchAdd}
}

// ValidStatuses returns all valid status values.
func ValidStatuses() []Status {
	return []Status{StatusNew, StatusDone, StatusNeedToGet, StatusAcquiring, StatusProcessing, StatusSyncing}
}

// IsValidCategory checks if a category string is valid.
func IsValidCategory(c string) bool {
	for _, v := range ValidCategories() {
		if string(v) == c {
			return true
		}
	}
	return false
}

// IsValidStatus checks if a status string is valid.
func IsValidStatus(s string) bool {
	for _, v := range ValidStatuses() {
		if string(v) == s {
			return true
		}
	}
	return false
}
