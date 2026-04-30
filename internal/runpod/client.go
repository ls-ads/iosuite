// Package runpod is a minimal GraphQL client for RunPod's API.
//
// Re-implements the operations iosuite needs (find/save/delete
// templates and serverless endpoints) in pure Go so the iosuite CLI
// has no Python dependency. Mirrors the surface of
// real-esrgan-serve's `build/runpod_deploy.py:RunPodClient`; lift
// changes from there if RunPod's schema shifts.
//
// Two RunPod hosts are in play:
//
//   - https://api.runpod.io/graphql — administrative ops (templates,
//     endpoints, account state). What this client talks to.
//   - https://api.runpod.ai/v2/{endpoint_id}/{run|status} — per-endpoint
//     job submission. Not used by this client; handled in the
//     `iosuite serve --provider runpod` path (round 3).
//
// Cloudflare in front of api.runpod.io blocks the default Go
// `User-Agent: Go-http-client/1.1` with a 1020 challenge. Setting a
// browser-shaped UA like `iosuite/<version>` gets through.
package runpod

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// GraphQLEndpoint is the admin API. Stable URL for years; if
	// RunPod ever rev's it they'll deprecate this one through their
	// changelog first.
	GraphQLEndpoint = "https://api.runpod.io/graphql"
)

// Client is a thin HTTP wrapper around the RunPod GraphQL endpoint.
// Construct with NewClient; methods are safe for concurrent use
// (http.Client is concurrent-safe and we don't share mutable state).
type Client struct {
	apiKey    string
	userAgent string
	http      *http.Client
}

// NewClient returns a Client. apiKey must be non-empty; callers
// resolve it from --runpod-api-key flag, RUNPOD_API_KEY env, or
// config file in that order before calling here.
func NewClient(apiKey, userAgent string) *Client {
	if userAgent == "" {
		userAgent = "iosuite/dev"
	}
	return &Client{
		apiKey:    apiKey,
		userAgent: userAgent,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

// query runs a GraphQL operation. Returns the `data` payload on
// success or an error describing the upstream failure (HTTP status,
// network blip, GraphQL `errors` array). Callers cast the returned
// any to whatever shape they expect from the operation.
func (c *Client) query(ctx context.Context, query string, variables map[string]any) (any, error) {
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GraphQLEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// Cloudflare blocks the default Go-http-client UA (see package
	// doc). Anything not on Cloudflare's bot list works; using
	// `iosuite/<version>` keeps the source of traffic obvious in
	// RunPod's request logs.
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("runpod graphql: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("runpod graphql: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runpod graphql: HTTP %d: %s",
			resp.StatusCode, truncate(string(respBody), 400))
	}
	var envelope struct {
		Data   any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("runpod graphql: parse: %w (body: %s)", err, truncate(string(respBody), 200))
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, len(envelope.Errors))
		for i, e := range envelope.Errors {
			msgs[i] = e.Message
		}
		return nil, fmt.Errorf("runpod graphql: %v", msgs)
	}
	return envelope.Data, nil
}

// Endpoint is the subset of an endpoint object we use for
// listing / lookup. Fields are lowercase JSON to match RunPod's
// schema; struct field names are PascalCase for Go conventions.
type Endpoint struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TemplateID string `json:"templateId"`
}

// Template is the subset we use. PodTemplates is the schema name —
// the same template type drives both pod and serverless endpoints
// when `isServerless: true` is set.
type Template struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ImageName string `json:"imageName"`
}

// ListEndpoints returns all serverless endpoints on the account.
// Used by `iosuite endpoint list` and by FindEndpoint for name-based
// lookup (RunPod doesn't expose a query-by-name).
func (c *Client) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	data, err := c.query(ctx, `query { myself { endpoints { id name templateId } } }`, nil)
	if err != nil {
		return nil, err
	}
	// Re-marshal then re-unmarshal to type-cast through the
	// any → struct shape. Avoids hand-walking the map[string]any.
	raw, _ := json.Marshal(data)
	var out struct {
		Myself struct {
			Endpoints []Endpoint `json:"endpoints"`
		} `json:"myself"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Myself.Endpoints, nil
}

// FindEndpoint returns the endpoint matching the given name, or nil
// if none exists. RunPod allows duplicate names but we treat the
// first match as authoritative — names are user-chosen and intended
// to be unique.
func (c *Client) FindEndpoint(ctx context.Context, name string) (*Endpoint, error) {
	all, err := c.ListEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, nil
}

// FindTemplate returns the (serverless) template with the given
// name, or nil if none exists.
func (c *Client) FindTemplate(ctx context.Context, name string) (*Template, error) {
	data, err := c.query(ctx, `query { myself { podTemplates { id name imageName } } }`, nil)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(data)
	var out struct {
		Myself struct {
			PodTemplates []Template `json:"podTemplates"`
		} `json:"myself"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	for i := range out.Myself.PodTemplates {
		if out.Myself.PodTemplates[i].Name == name {
			return &out.Myself.PodTemplates[i], nil
		}
	}
	return nil, nil
}

// SaveTemplate creates a new template or updates an existing one
// (when ExistingID is non-empty). Returns the template id.
type SaveTemplateInput struct {
	Name             string
	Image            string
	ContainerDiskGB  int
	ExistingID       string
	RegistryAuthID   string // empty = no registry auth (public images)
	Env              []EnvVar
}

// EnvVar is one container env entry.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (c *Client) SaveTemplate(ctx context.Context, in SaveTemplateInput) (string, error) {
	if in.ContainerDiskGB == 0 {
		in.ContainerDiskGB = 10 // sane default; the runpod-trt image is ~3 GB
	}
	// RunPod's saveTemplate input requires `env` to be a non-null
	// list ([EnvironmentVariableInput]!). A nil Go slice marshals to
	// JSON null, which the GraphQL validator rejects with "Expected
	// value of type [EnvironmentVariableInput]!, found null". Force
	// the empty-list shape.
	env := in.Env
	if env == nil {
		env = []EnvVar{}
	}
	envBytes, _ := json.Marshal(env)

	idField := ""
	if in.ExistingID != "" {
		idField = fmt.Sprintf(`id: "%s",`, escapeGQL(in.ExistingID))
	}
	authField := ""
	if in.RegistryAuthID != "" {
		authField = fmt.Sprintf(`containerRegistryAuthId: "%s",`, escapeGQL(in.RegistryAuthID))
	}
	mutation := fmt.Sprintf(`
		mutation saveTemplate {
			saveTemplate(input: {
				%s
				%s
				name: "%s",
				imageName: "%s",
				containerDiskInGb: %d,
				volumeInGb: 0,
				dockerArgs: "",
				isServerless: true,
				env: %s
			}) { id name }
		}
	`, idField, authField, escapeGQL(in.Name), escapeGQL(in.Image), in.ContainerDiskGB, string(envBytes))

	data, err := c.query(ctx, mutation, nil)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(data)
	var out struct {
		SaveTemplate struct {
			ID string `json:"id"`
		} `json:"saveTemplate"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	return out.SaveTemplate.ID, nil
}

// SaveEndpoint creates or updates a serverless endpoint. GPU IDs are
// the comma-separated pool name(s) like "ADA_24" — see
// internal/endpoint/runpod.go for the GPU-class → pool mapping.
type SaveEndpointInput struct {
	Name         string
	TemplateID   string
	GPUPool      string // e.g. "ADA_24"
	WorkersMin   int
	WorkersMax   int
	IdleTimeoutS int
	// Flashboot enables RunPod FlashBoot — workers resume from a
	// snapshot rather than re-downloading the image and re-loading the
	// model. Drops cold-start from ~45 s to ~5 s on the real-esrgan
	// trt image. Defaults true; pass false explicitly to opt out.
	Flashboot  bool
	ExistingID string
}

func (c *Client) SaveEndpoint(ctx context.Context, in SaveEndpointInput) (string, error) {
	if in.IdleTimeoutS == 0 {
		in.IdleTimeoutS = 30 // RunPod's recommended default
	}
	if in.WorkersMax == 0 {
		in.WorkersMax = 1 // safe default; ops can scale up via console or update
	}
	idField := ""
	if in.ExistingID != "" {
		idField = fmt.Sprintf(`id: "%s",`, escapeGQL(in.ExistingID))
	}
	// FlashBoot is exposed on RunPod's GraphQL API as an enum, not a
	// boolean — `flashBootType: FLASHBOOT` (on) vs `flashBootType: OFF`.
	// Both values discovered empirically: introspection is disabled
	// on RunPod's Apollo server, and only those two pass validation.
	// (RunPod's newer REST API at rest.runpod.io takes a plain
	// `flashboot: bool`; we stay on GraphQL here for parity with the
	// rest of this client until a broader REST migration.)
	flashBootType := "OFF"
	if in.Flashboot {
		flashBootType = "FLASHBOOT"
	}
	mutation := fmt.Sprintf(`
		mutation saveEndpoint {
			saveEndpoint(input: {
				%s
				name: "%s",
				templateId: "%s",
				gpuIds: "%s",
				workersMin: %d,
				workersMax: %d,
				idleTimeout: %d,
				flashBootType: %s,
				scalerType: "QUEUE_DELAY",
				scalerValue: 4,
				networkVolumeId: ""
			}) { id }
		}
	`, idField, escapeGQL(in.Name), escapeGQL(in.TemplateID), escapeGQL(in.GPUPool),
		in.WorkersMin, in.WorkersMax, in.IdleTimeoutS, flashBootType)

	data, err := c.query(ctx, mutation, nil)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(data)
	var out struct {
		SaveEndpoint struct {
			ID string `json:"id"`
		} `json:"saveEndpoint"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	return out.SaveEndpoint.ID, nil
}

// DeleteEndpoint removes the endpoint with the given id. RunPod
// drains workers internally; the call returns immediately on
// success (a few hundred ms typically).
func (c *Client) DeleteEndpoint(ctx context.Context, id string) error {
	mutation := fmt.Sprintf(`mutation { deleteEndpoint(id: "%s") }`, escapeGQL(id))
	_, err := c.query(ctx, mutation, nil)
	return err
}

// escapeGQL is the minimal escape we need for the inline values we
// inject into mutation strings. RunPod's API doesn't require fully
// general JSON escaping of these fields (names are user-chosen but
// constrained to a friendly subset), so this is a surface-area bug
// guard, not a security boundary — operators with valid API keys
// can already do anything to their account.
func escapeGQL(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	return string(out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
