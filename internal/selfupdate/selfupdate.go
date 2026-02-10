package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	defaultOwner   = "safedep"
	defaultRepo    = "gryph"
	defaultTimeout = 5 * time.Second
	defaultBaseURL = "https://api.github.com"
)

type CheckInput struct {
	Version string
}

type UpdateResult struct {
	UpdateAvailable bool   `json:"update_available"`
	LatestVersion   string `json:"latest_version"`
	CurrentVersion  string `json:"current_version"`
	ReleaseURL      string `json:"release_url"`
}

type Option func(*Checker)

type Checker struct {
	owner   string
	repo    string
	timeout time.Duration
	baseURL string
	client  *http.Client
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Checker) {
		c.client = client
	}
}

func WithBaseURL(baseURL string) Option {
	return func(c *Checker) {
		c.baseURL = baseURL
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Checker) {
		c.timeout = timeout
	}
}

func NewChecker(opts ...Option) *Checker {
	c := &Checker{
		owner:   defaultOwner,
		repo:    defaultRepo,
		timeout: defaultTimeout,
		baseURL: defaultBaseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.client == nil {
		c.client = &http.Client{Timeout: c.timeout}
	}
	return c
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func (c *Checker) Check(ctx context.Context, input *CheckInput) (*UpdateResult, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", strings.TrimRight(c.baseURL, "/"), c.owner, c.repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	result := &UpdateResult{
		LatestVersion:  release.TagName,
		CurrentVersion: input.Version,
		ReleaseURL:     release.HTMLURL,
	}

	if isNewer(input.Version, release.TagName) {
		result.UpdateAvailable = true
	}

	return result, nil
}

func (c *Checker) CheckAsync(ctx context.Context, input *CheckInput) <-chan *UpdateResult {
	ch := make(chan *UpdateResult, 1)
	go func() {
		defer close(ch)
		result, err := c.Check(ctx, input)
		if err != nil {
			return
		}
		ch <- result
	}()
	return ch
}

func isNewer(current, latest string) bool {
	current = normalize(current)
	latest = normalize(latest)

	if !semver.IsValid(current) || !semver.IsValid(latest) {
		return false
	}

	return semver.Compare(latest, current) > 0
}

func normalize(v string) string {
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}
