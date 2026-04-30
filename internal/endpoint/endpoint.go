// Package endpoint implements `iosuite endpoint deploy/list/destroy`.
//
// Manages remote provider endpoints (RunPod first; vast.ai / Modal
// later) so users can stand up the GPU side of the stack with one
// command.
//
// What `iosuite endpoint deploy --provider runpod --tool real-esrgan`
// does:
//
//  1. Resolve the tool's deploy manifest. The caller (cobra layer)
//     fetches a deploy/runpod.json from the *-serve repo at the
//     requested git tag (see internal/registry + internal/manifest)
//     and passes it via DeployInput.Manifest. iosuite holds NO
//     per-tool implementation knowledge — image, disk, GPU pools,
//     CUDA pin, FlashBoot default all flow from the manifest.
//  2. Look up (or create) a RunPod template with the manifest's
//     image and disk.
//  3. Look up (or create) a serverless endpoint named after `--name`
//     (default: `<tool>-<gpu-class>`) wired to that template, on
//     the GPU pool the manifest declares for `--gpu-class`.
//  4. Print the endpoint id + ready-to-paste env line.
//
// Cold-start + warm-latency measurement lives in `iosuite endpoint
// benchmark` (internal/benchmark), driven by deploy/benchmark.json
// on the *-serve side.
package endpoint

import (
	"context"
	"fmt"
	"io"
	"strings"

	"iosuite.io/internal/manifest"
	"iosuite.io/internal/runpod"
)

// Provider names. Round 2 implements RunPod only; the constants
// exist so the cobra layer can validate `--provider` against a
// closed set today.
const (
	ProviderRunPod = "runpod"
)

// DeployInput captures everything `iosuite endpoint deploy` needs.
// Manifest is required — the caller resolves it before calling
// Deploy(). All per-tool knowledge lives there, not here.
type DeployInput struct {
	Provider string // ProviderRunPod
	Tool     string // "real-esrgan" — used only for naming/UA
	GPUClass string // "rtx-4090" etc; mapped to a pool via Manifest.GPUPools
	Name     string // endpoint name; auto-derived if empty
	APIKey   string // RunPod API key
	// Manifest is the parsed deploy spec. Caller owns fetching and
	// validation (typically via internal/registry + internal/manifest).
	Manifest *manifest.Manifest
	// Per-call overrides. Each defaults to the matching field on
	// Manifest.Endpoint when zero/nil.
	WorkersMax   int
	IdleTimeoutS int
	// Flashboot is *bool so "user didn't pass --flashboot" (nil) is
	// distinguishable from explicit --flashboot=false. nil → use
	// the manifest's default.
	Flashboot *bool
	// MinCudaVersion overrides the manifest's value. Empty = use
	// the manifest's value (which itself may be empty for tools
	// that don't pin a driver).
	MinCudaVersion string
	UserAgent      string // surfaced to RunPod logs; iosuite/<version>
}

// DeployResult carries the outputs of a successful deploy.
type DeployResult struct {
	EndpointID     string
	EndpointName   string
	TemplateID     string
	Image          string
	GPUPool        string
	Flashboot      bool
	MinCudaVersion string
	ManifestSource string // URL or filepath the manifest came from; informational
}

// Deploy runs the full create-or-update flow. Idempotent: re-running
// with the same name updates the existing template + endpoint
// in-place, picking up image / scaler changes between iosuite
// releases. Caller writes the human-friendly output (we return
// structured data so they can format it).
func Deploy(ctx context.Context, in DeployInput) (*DeployResult, error) {
	if in.Provider != ProviderRunPod {
		return nil, fmt.Errorf("provider %q is not supported (only 'runpod' is implemented)", in.Provider)
	}
	if in.APIKey == "" {
		return nil, fmt.Errorf("RunPod API key required (--runpod-api-key, RUNPOD_API_KEY, or [runpod] api_key in config)")
	}
	if in.Manifest == nil {
		return nil, fmt.Errorf("DeployInput.Manifest is required — the cobra layer should resolve it via internal/registry + internal/manifest before calling Deploy")
	}
	m := in.Manifest

	pool, ok := m.GPUPools[in.GPUClass]
	if !ok {
		known := make([]string, 0, len(m.GPUPools))
		for k := range m.GPUPools {
			known = append(known, k)
		}
		return nil, fmt.Errorf("gpu-class %q not declared by the %s manifest. Known: %s",
			in.GPUClass, m.Tool, strings.Join(known, ", "))
	}

	name := in.Name
	if name == "" {
		// Default endpoint name: <tool>-<gpu-class>. Stable so
		// re-running deploy without --name updates the same
		// endpoint instead of cluttering the account with copies.
		name = fmt.Sprintf("%s-%s", in.Tool, in.GPUClass)
	}
	templateName := name + "-tmpl"

	rp := runpod.NewClient(in.APIKey, in.UserAgent)

	// Template — find or save. Image + disk + env all come from the
	// manifest so a tool bump (e.g. new image tag, new env) lands by
	// pushing a new manifest, not by patching iosuite.
	existingTmpl, err := rp.FindTemplate(ctx, templateName)
	if err != nil {
		return nil, fmt.Errorf("look up template: %w", err)
	}
	tmplInput := runpod.SaveTemplateInput{
		Name:            templateName,
		Image:           m.Image,
		ContainerDiskGB: m.Endpoint.ContainerDiskGB,
		Env:             toRunpodEnv(m.Env),
	}
	if existingTmpl != nil {
		tmplInput.ExistingID = existingTmpl.ID
	}
	templateID, err := rp.SaveTemplate(ctx, tmplInput)
	if err != nil {
		return nil, fmt.Errorf("save template: %w", err)
	}

	// Endpoint — find or save. Defaults from the manifest, overridable
	// per call.
	existing, err := rp.FindEndpoint(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("look up endpoint: %w", err)
	}
	flashboot := m.Endpoint.FlashbootDefault
	if in.Flashboot != nil {
		flashboot = *in.Flashboot
	}
	minCuda := m.Endpoint.MinCudaVersion
	if in.MinCudaVersion != "" {
		minCuda = in.MinCudaVersion
	}
	epInput := runpod.SaveEndpointInput{
		Name:           name,
		TemplateID:     templateID,
		GPUPool:        pool,
		WorkersMin:     0,
		WorkersMax:     defaultIfZero(in.WorkersMax, m.Endpoint.WorkersMaxDefault),
		IdleTimeoutS:   defaultIfZero(in.IdleTimeoutS, m.Endpoint.IdleTimeoutSDefault),
		Flashboot:      flashboot,
		MinCudaVersion: minCuda,
	}
	if existing != nil {
		epInput.ExistingID = existing.ID
	}
	endpointID, err := rp.SaveEndpoint(ctx, epInput)
	if err != nil {
		return nil, fmt.Errorf("save endpoint: %w", err)
	}

	return &DeployResult{
		EndpointID:     endpointID,
		EndpointName:   name,
		TemplateID:     templateID,
		Image:          m.Image,
		GPUPool:        pool,
		Flashboot:      flashboot,
		MinCudaVersion: minCuda,
	}, nil
}

// List returns every serverless endpoint on the configured account.
// Round 2 supports RunPod only; the cobra layer enforces that.
func List(ctx context.Context, provider, apiKey, userAgent string) ([]runpod.Endpoint, error) {
	if provider != ProviderRunPod {
		return nil, fmt.Errorf("provider %q is not supported", provider)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("RunPod API key required (--runpod-api-key, RUNPOD_API_KEY, or [runpod] api_key in config)")
	}
	rp := runpod.NewClient(apiKey, userAgent)
	return rp.ListEndpoints(ctx)
}

// Destroy deletes the endpoint with the given id (or, when id is
// empty and name is provided, the endpoint matching that name).
// Returns the ID it actually deleted so the caller can confirm.
func Destroy(ctx context.Context, provider, apiKey, userAgent, id, name string) (string, error) {
	if provider != ProviderRunPod {
		return "", fmt.Errorf("provider %q is not supported", provider)
	}
	if apiKey == "" {
		return "", fmt.Errorf("RunPod API key required (--runpod-api-key, RUNPOD_API_KEY, or [runpod] api_key in config)")
	}
	rp := runpod.NewClient(apiKey, userAgent)

	target := id
	if target == "" {
		if name == "" {
			return "", fmt.Errorf("must provide either an endpoint id or --name")
		}
		ep, err := rp.FindEndpoint(ctx, name)
		if err != nil {
			return "", fmt.Errorf("look up endpoint by name: %w", err)
		}
		if ep == nil {
			return "", fmt.Errorf("no endpoint named %q on this account", name)
		}
		target = ep.ID
	}
	if err := rp.DeleteEndpoint(ctx, target); err != nil {
		return "", fmt.Errorf("delete endpoint %s: %w", target, err)
	}
	return target, nil
}

// PrintDeploy writes a human-friendly summary of a Deploy result.
// Separated from Deploy() so callers wanting structured output
// (json, etc.) can format it differently without re-running.
func PrintDeploy(w io.Writer, r *DeployResult) {
	fmt.Fprintln(w, "deployed:")
	fmt.Fprintf(w, "  endpoint name: %s\n", r.EndpointName)
	fmt.Fprintf(w, "  endpoint id:   %s\n", r.EndpointID)
	fmt.Fprintf(w, "  template id:   %s\n", r.TemplateID)
	fmt.Fprintf(w, "  image:         %s\n", r.Image)
	fmt.Fprintf(w, "  gpu pool:      %s\n", r.GPUPool)
	fmt.Fprintf(w, "  flashboot:     %t\n", r.Flashboot)
	if r.MinCudaVersion != "" {
		fmt.Fprintf(w, "  min cuda:      %s (driver-pinned)\n", r.MinCudaVersion)
	}
	if r.ManifestSource != "" {
		fmt.Fprintf(w, "  manifest:      %s\n", r.ManifestSource)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "ready-to-use env line for clients:")
	fmt.Fprintf(w, "  export RUNPOD_ENDPOINT_ID=%s\n", r.EndpointID)
}

func defaultIfZero(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// toRunpodEnv converts manifest env entries to the runpod-client
// shape. Two structs because the manifest package shouldn't depend
// on the runpod client (would entangle two packages that otherwise
// have no shared concerns).
func toRunpodEnv(in []manifest.EnvVar) []runpod.EnvVar {
	out := make([]runpod.EnvVar, len(in))
	for i, e := range in {
		out[i] = runpod.EnvVar{Key: e.Key, Value: e.Value}
	}
	return out
}
