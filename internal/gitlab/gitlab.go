// Package gitlab is a small client of the GitLab REST API (v4) for merge
// requests (GITX-39): list the open ones of a project and create one.
//
// The token is sent only in the PRIVATE-TOKEN header. It comes from the
// caller (the app reads it from internal/secrets) and is never put into a
// URL, a log or an error.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ErrUnauthorized: GitLab refused the token (401).
var ErrUnauthorized = errors.New("GitLab refused the token (it is wrong, expired or lacks the api scope)")

// ErrNotFound: no such project, or the token cannot see it (404).
var ErrNotFound = errors.New("GitLab has no such project, or the token cannot see it")

// Project is a GitLab project: its host and its full path (group/sub/name).
type Project struct{ Host, Path string }

// FromWebBase reads a project from a web base (https://host/group/name,
// as git.WebBase gives it). Only a host whose name has "gitlab" in it
// counts as GitLab.
func FromWebBase(base string) (Project, bool) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Hostname() == "" || !strings.Contains(strings.ToLower(u.Hostname()), "gitlab") {
		return Project{}, false
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return Project{}, false
	}
	return Project{Host: u.Host, Path: p}, true
}

// MR is one merge request.
type MR struct {
	IID            int
	Title, Author  string
	Source, Target string // branches
	URL, State     string
	Draft          bool
}

// NewMR is what CreateMR sends.
type NewMR struct {
	Source, Target, Title, Description string
	Draft                              bool
}

// Client talks to one GitLab host.
type Client struct {
	api   string // https://host, without /api/v4
	token string
	HTTP  *http.Client
}

// New is a client of the GitLab at api (https://gitlab.com).
func New(api, token string) *Client {
	return &Client{api: strings.TrimRight(api, "/"), token: token, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// ListLimit caps the list.
const ListLimit = 50

type mrJSON struct {
	IID    int    `json:"iid"`
	Title  string `json:"title"`
	Author struct {
		Username string `json:"username"`
	} `json:"author"`
	Source string `json:"source_branch"`
	Target string `json:"target_branch"`
	URL    string `json:"web_url"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
}

func (j mrJSON) mr() MR {
	return MR{IID: j.IID, Title: j.Title, Author: j.Author.Username, Source: j.Source, Target: j.Target, URL: j.URL, State: j.State, Draft: j.Draft}
}

// ListMRs reads the open merge requests of project, newest first.
func (c *Client) ListMRs(ctx context.Context, project string) ([]MR, error) {
	var raw []mrJSON
	q := fmt.Sprintf("?state=opened&order_by=created_at&sort=desc&per_page=%d", ListLimit)
	if err := c.do(ctx, http.MethodGet, c.mrPath(project)+q, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]MR, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.mr())
	}
	return out, nil
}

// CreateMR opens a merge request; a draft gets GitLab's "Draft: " title.
func (c *Client) CreateMR(ctx context.Context, project string, in NewMR) (MR, error) {
	title := strings.TrimSpace(in.Title)
	if in.Source == "" || in.Target == "" || title == "" {
		return MR{}, errors.New("a merge request needs a source branch, a target branch and a title")
	}
	if in.Draft && !strings.HasPrefix(strings.ToLower(title), "draft:") {
		title = "Draft: " + title
	}
	body, err := json.Marshal(map[string]any{
		"source_branch": in.Source, "target_branch": in.Target, "title": title,
		"description": in.Description, "remove_source_branch": false,
	})
	if err != nil {
		return MR{}, err
	}
	var raw mrJSON
	if err := c.do(ctx, http.MethodPost, c.mrPath(project), body, &raw); err != nil {
		return MR{}, err
	}
	return raw.mr(), nil
}

func (c *Client) mrPath(project string) string {
	return c.api + "/api/v4/projects/" + url.PathEscape(project) + "/merge_requests"
}

func (c *Client) do(ctx context.Context, method, u string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return errors.New("not a valid GitLab address")
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// The URL has no token; only the cause is kept.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("could not reach GitLab: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("could not read GitLab's answer: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 300:
		return errors.New("GitLab: " + c.message(data, resp.Status))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("GitLab sent an answer that is not JSON")
	}
	return nil
}

// message is GitLab's error text ({"message": "…"}, a list or a map of
// field errors), one short line with the token taken out.
func (c *Client) message(data []byte, status string) string {
	var e struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	text := status
	if json.Unmarshal(data, &e) == nil {
		switch v := e.Message.(type) {
		case string:
			text = v
		case []any:
			var parts []string
			for _, p := range v {
				parts = append(parts, fmt.Sprint(p))
			}
			text = strings.Join(parts, "; ")
		case map[string]any:
			var parts []string
			for k, p := range v {
				parts = append(parts, k+" "+fmt.Sprint(p))
			}
			sort.Strings(parts)
			text = strings.Join(parts, "; ")
		default:
			if e.Error != "" {
				text = e.Error
			}
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if c.token != "" {
		text = strings.ReplaceAll(text, c.token, "[token]")
	}
	if r := []rune(text); len(r) > 200 {
		text = string(r[:200]) + "…"
	}
	return text
}
