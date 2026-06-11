package types

import "time"

const (
	ResourceScopeServer   = "server"
	ResourceScopePersonal = "personal"

	ResourceSourceAdmin = "admin"
	ResourceSourceUser  = "user"

	VisibilityAll        = "all"
	VisibilityRestricted = "restricted"

	IconModeUploaded = "uploaded"
	IconModeFetched  = "fetched"
	IconModeLetter   = "letter"
)

type Resource struct {
	ID            string         `json:"id" gorm:"primaryKey"`
	AccountID     string         `json:"-" gorm:"index"`
	UserID        string         `json:"-" gorm:"index"`
	Scope         string         `json:"scope" gorm:"index"`
	Name          string         `json:"name"`
	Category      string         `json:"category"`
	Description   string         `json:"description"`
	IconURL       string         `json:"iconUrl"`
	IconMode      string         `json:"iconMode"`
	URL           string         `json:"url"`
	Tags          []string       `json:"tags" gorm:"serializer:json"`
	Enabled       bool           `json:"enabled"`
	Favorite      bool           `json:"favorite"`
	Sort          int            `json:"sort"`
	Source        string         `json:"source"`
	Visibility    string         `json:"visibility,omitempty"`
	VisibleGroups []string       `json:"visibleGroups,omitempty" gorm:"-"`
	VisibleUsers  []string       `json:"visibleUsers,omitempty" gorm:"-"`
	Metadata      map[string]any `json:"metadata" gorm:"serializer:json"`
	CreatedBy     string         `json:"createdBy,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

func (Resource) TableName() string {
	return "workbench_resources"
}

type ResourceVisibleGroup struct {
	AccountID  string    `gorm:"primaryKey"`
	ResourceID string    `gorm:"primaryKey;index"`
	GroupID    string    `gorm:"primaryKey;index"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (ResourceVisibleGroup) TableName() string {
	return "workbench_resource_visible_groups"
}

type ResourceVisibleUser struct {
	AccountID  string    `gorm:"primaryKey"`
	ResourceID string    `gorm:"primaryKey;index"`
	UserID     string    `gorm:"primaryKey;index"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (ResourceVisibleUser) TableName() string {
	return "workbench_resource_visible_users"
}

type UserResources struct {
	AccountID     string     `gorm:"primaryKey"`
	UserID        string     `gorm:"primaryKey"`
	ResourcesJSON []Resource `gorm:"serializer:json"`
	RecentVisits  []RecentVisit `gorm:"serializer:json"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (UserResources) TableName() string {
	return "workbench_user_resources"
}

type Asset struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	AccountID   string    `json:"-" gorm:"index"`
	OwnerUserID string    `json:"-" gorm:"index"`
	SourceURL   string    `json:"sourceUrl"`
	StoragePath string    `json:"-"`
	PublicURL   string    `json:"publicUrl"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (Asset) TableName() string {
	return "workbench_assets"
}

type ResourceList struct {
	ServerResources   []Resource `json:"serverResources"`
	PersonalResources []Resource `json:"personalResources"`
	Categories        []Category `json:"categories"`
	Version           int        `json:"version"`
}

type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Sort int    `json:"sort"`
}

type RecentVisit struct {
	ResourceID string    `json:"resourceId"`
	Scope      string    `json:"scope"`
	VisitedAt  time.Time `json:"visitedAt"`
}

type IconAssetResult struct {
	IconURL  string `json:"iconUrl"`
	IconMode string `json:"iconMode"`
}
