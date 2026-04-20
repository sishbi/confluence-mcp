package confluence

import "time"

type Space struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Page struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	SpaceID  string      `json:"spaceId"`
	ParentID string      `json:"parentId"`
	Status   string      `json:"status"`
	Version  PageVersion `json:"version"`
	Body     PageBody    `json:"body"`
}

type PageVersion struct {
	Number  int       `json:"number"`
	Message string    `json:"message"`
	Created time.Time `json:"createdAt"`
}

type PageBody struct {
	Storage StorageBody `json:"storage"`
}

type StorageBody struct {
	Representation string `json:"representation"`
	Value          string `json:"value"`
}

type Comment struct {
	ID      string         `json:"id"`
	Body    PageBody       `json:"body"`
	PageID  string         `json:"pageId"`
	Version CommentVersion `json:"version"`
	Created time.Time      `json:"createdAt"`
}

type CommentVersion struct {
	Number  int       `json:"number"`
	Created time.Time `json:"createdAt"`
}

type Label struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
}

type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"publicName"` // v1 API uses "publicName", v2 uses "displayName"
	Email       string `json:"email"`
}

type PaginatedResponse[T any] struct {
	Results []T   `json:"results"`
	Links   Links `json:"_links"`
}

type Links struct {
	Next string `json:"next"`
}

type SearchResult struct {
	Results   []SearchResultItem `json:"results"`
	TotalSize int                `json:"totalSize"`
	Links     Links              `json:"_links"`
}

type SearchResultItem struct {
	Content SearchContent `json:"content"`
	Title   string        `json:"title"`
	Excerpt string        `json:"excerpt"`
	URL     string        `json:"url"`
}

type SearchContent struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

type ListOptions struct {
	Limit  int
	Cursor string
}
