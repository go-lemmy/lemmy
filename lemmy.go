// Package lemmy is a dependency-free read client for the Lemmy REST API
// (/api/v3). It targets CGO_ENABLED=0 builds and uses only the Go standard
// library.
package lemmy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a read client for a single Lemmy instance.
type Client struct {
	// Instance is the base URL of the Lemmy instance,
	// e.g. https://lemmy.world.
	Instance string
	// HTTPClient is used for all requests. Defaults to http.DefaultClient.
	HTTPClient *http.Client
	// UserAgent is sent with every request.
	UserAgent string

	jwt string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the http.Client used for requests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.HTTPClient = h }
}

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.UserAgent = ua }
}

// New returns a Client for the given instance base URL (e.g.
// https://lemmy.world). Trailing slashes are trimmed.
func New(instance string, opts ...Option) *Client {
	c := &Client{
		Instance:   strings.TrimRight(instance, "/"),
		HTTPClient: http.DefaultClient,
		UserAgent:  "go-lemmy/lemmy",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// do executes an HTTP request against the given API path (relative to
// /api/v3) and decodes a 2xx JSON body into out. Non-2xx responses and
// decode failures are returned as errors.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, out any) error {
	u := c.Instance + "/api/v3" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.jwt != "" {
		req.Header.Set("Authorization", "Bearer "+c.jwt)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(data)
		if len(snippet) > 512 {
			snippet = snippet[:512]
		}
		return fmt.Errorf("lemmy: %s %s: unexpected status %d: %s", method, path, resp.StatusCode, snippet)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("lemmy: decode %s %s: %w", method, path, err)
	}
	return nil
}

// Login POSTs /api/v3/user/login and stores the returned jwt for
// subsequent requests.
func (c *Client) Login(ctx context.Context, usernameOrEmail, password string) error {
	reqBody := map[string]string{
		"username_or_email": usernameOrEmail,
		"password":          password,
	}
	buf, _ := json.Marshal(reqBody) // a map[string]string never fails to marshal
	var out struct {
		JWT string `json:"jwt"`
	}
	if err := c.do(ctx, http.MethodPost, "/user/login", nil, bytes.NewReader(buf), &out); err != nil {
		return err
	}
	c.jwt = out.JWT
	return nil
}

// Post is a flattened view of a Lemmy post.
type Post struct {
	ID           int
	Title        string
	URL          string
	Body         string
	Permalink    string
	ThumbnailURL string
	Published    time.Time
	NSFW         bool
	Creator      string
	Community    string
	Score        int
	Comments     int
}

// PostList is a list of posts.
type PostList struct {
	Posts []Post
}

// Community is one community-search hit: enough to show a discovery row and
// subscribe. Name is the local handle ("golang"); ActorID is the federated URL
// ("https://lemmy.world/c/golang") that identifies it across instances.
type Community struct {
	Name        string
	Title       string
	Description string
	Icon        string
	ActorID     string
	Subscribers int
	NSFW        bool
}

// CommunityList is a page of community-search results.
type CommunityList struct {
	Communities []Community
}

// PostsOptions configures a Posts request.
type PostsOptions struct {
	Community string // community name; empty = instance-wide
	Sort      string // Active|Hot|New|Top...; default "Hot"
	Type      string // All|Local|Subscribed; default "All"
	Limit     int    // default 20
	Page      int    // 1-based; default 1
}

// postListResponse mirrors the JSON shape of /api/v3/post/list.
type postListResponse struct {
	Posts []struct {
		Post struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			URL          string `json:"url"`
			Body         string `json:"body"`
			APID         string `json:"ap_id"`
			ThumbnailURL string `json:"thumbnail_url"`
			Published    string `json:"published"`
			NSFW         bool   `json:"nsfw"`
		} `json:"post"`
		Creator struct {
			Name string `json:"name"`
		} `json:"creator"`
		Community struct {
			Name string `json:"name"`
		} `json:"community"`
		Counts struct {
			Score    int `json:"score"`
			Comments int `json:"comments"`
		} `json:"counts"`
	} `json:"posts"`
}

// searchCommunitiesResponse mirrors the community slice of /api/v3/search.
type searchCommunitiesResponse struct {
	Communities []struct {
		Community struct {
			Name        string `json:"name"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Icon        string `json:"icon"`
			ActorID     string `json:"actor_id"`
			NSFW        bool   `json:"nsfw"`
		} `json:"community"`
		Counts struct {
			Subscribers int `json:"subscribers"`
		} `json:"counts"`
	} `json:"communities"`
}

// SearchCommunities GETs /api/v3/search?type_=Communities for communities
// matching query — a public read used to discover communities to subscribe to.
// limit caps the page (0 → 20).
func (c *Client) SearchCommunities(ctx context.Context, query string, limit int) (*CommunityList, error) {
	if limit == 0 {
		limit = 20
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("type_", "Communities")
	q.Set("limit", strconv.Itoa(limit))

	var raw searchCommunitiesResponse
	if err := c.do(ctx, http.MethodGet, "/search", q, nil, &raw); err != nil {
		return nil, err
	}
	out := &CommunityList{Communities: make([]Community, 0, len(raw.Communities))}
	for _, v := range raw.Communities {
		out.Communities = append(out.Communities, Community{
			Name:        v.Community.Name,
			Title:       v.Community.Title,
			Description: v.Community.Description,
			Icon:        v.Community.Icon,
			ActorID:     v.Community.ActorID,
			Subscribers: v.Counts.Subscribers,
			NSFW:        v.Community.NSFW,
		})
	}
	return out, nil
}

// Posts GETs /api/v3/post/list with the options as query params.
func (c *Client) Posts(ctx context.Context, opts PostsOptions) (*PostList, error) {
	sort := opts.Sort
	if sort == "" {
		sort = "Hot"
	}
	typ := opts.Type
	if typ == "" {
		typ = "All"
	}
	limit := opts.Limit
	if limit == 0 {
		limit = 20
	}
	page := opts.Page
	if page == 0 {
		page = 1
	}

	q := url.Values{}
	q.Set("sort", sort)
	q.Set("type_", typ)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", strconv.Itoa(page))
	if opts.Community != "" {
		q.Set("community_name", opts.Community)
	}

	var raw postListResponse
	if err := c.do(ctx, http.MethodGet, "/post/list", q, nil, &raw); err != nil {
		return nil, err
	}

	list := &PostList{Posts: make([]Post, 0, len(raw.Posts))}
	for _, v := range raw.Posts {
		list.Posts = append(list.Posts, Post{
			ID:           v.Post.ID,
			Title:        v.Post.Name,
			URL:          v.Post.URL,
			Body:         v.Post.Body,
			Permalink:    v.Post.APID,
			ThumbnailURL: v.Post.ThumbnailURL,
			Published:    parseTime(v.Post.Published),
			NSFW:         v.Post.NSFW,
			Creator:      v.Creator.Name,
			Community:    v.Community.Name,
			Score:        v.Counts.Score,
			Comments:     v.Counts.Comments,
		})
	}
	return list, nil
}

// parseTime parses Lemmy timestamps tolerantly, returning the zero time on
// failure.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
