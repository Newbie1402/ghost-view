package model

import "time"

type Platform string

const (
	TikTok    Platform = "tiktok"
	Instagram Platform = "instagram"
	Facebook  Platform = "facebook"
)

func (p Platform) Valid() bool { return p == TikTok || p == Instagram || p == Facebook }

type AccessStatus string

const (
	AccessPublic      AccessStatus = "PUBLIC"
	AccessPrivate     AccessStatus = "PRIVATE"
	AccessUnavailable AccessStatus = "UNAVAILABLE"
)

type MediaType string

const (
	Image         MediaType = "IMAGE"
	Video         MediaType = "VIDEO"
	Story         MediaType = "STORY"
	Reel          MediaType = "REEL"
	HighlightType MediaType = "HIGHLIGHT"
)

type ProviderCapabilities struct {
	Search     bool `json:"search"`
	Profile    bool `json:"profile"`
	Posts      bool `json:"posts"`
	Reposts    bool `json:"reposts"`
	Stories    bool `json:"stories"`
	Highlights bool `json:"highlights"`
	Downloads  bool `json:"downloads"`
}
type ProviderInfo struct {
	DataSource   string               `json:"dataSource"`
	Platform     Platform             `json:"platform"`
	Status       string               `json:"status"`
	Capabilities ProviderCapabilities `json:"capabilities"`
	Message      string               `json:"message,omitempty"`
}
type ProfileSummary struct {
	DataSource  string   `json:"dataSource"`
	Platform    Platform `json:"platform"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	AvatarURL   string   `json:"avatarURL,omitempty"`
	Verified    bool     `json:"verified"`
}
type Profile struct {
	DataSource     string       `json:"dataSource"`
	Platform       Platform     `json:"platform"`
	Username       string       `json:"username"`
	DisplayName    string       `json:"displayName"`
	Bio            string       `json:"bio,omitempty"`
	AvatarURL      string       `json:"avatarURL,omitempty"`
	ProfileURL     string       `json:"profileURL,omitempty"`
	Verified       bool         `json:"verified"`
	FollowerCount  *int64       `json:"followerCount,omitempty"`
	FollowingCount *int64       `json:"followingCount,omitempty"`
	PostCount      *int64       `json:"postCount,omitempty"`
	AccessStatus   AccessStatus `json:"accessStatus"`
	Message        string       `json:"message,omitempty"`
}
type MediaItem struct {
	PreviewOnly  bool      `json:"previewOnly,omitempty"`
	ID           string    `json:"id"`
	Type         MediaType `json:"type"`
	ThumbnailURL string    `json:"thumbnailURL,omitempty"`
	MediaURL     string    `json:"mediaURL,omitempty"`
	Width        int       `json:"width,omitempty"`
	Height       int       `json:"height,omitempty"`
	Duration     float64   `json:"duration,omitempty"`
	CreatedAt    time.Time `json:"createdAt,omitzero"`
	Caption      string    `json:"caption,omitempty"`
	Downloadable bool      `json:"downloadable"`
}
type MediaPage struct {
	Items      []MediaItem `json:"items"`
	NextCursor string      `json:"nextCursor,omitempty"`
}
type Highlight struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	ThumbnailURL string      `json:"thumbnailURL"`
	Items        []MediaItem `json:"items"`
}
type DownloadResource struct {
	URL          string
	AllowedHosts []string
	Filename     string
	ContentType  string
	Username     string
	Referer      string
	Fixture      []byte
}
type SearchResult struct {
	DataSource string           `json:"dataSource"`
	Platform   Platform         `json:"platform"`
	Profiles   []ProfileSummary `json:"profiles"`
}
