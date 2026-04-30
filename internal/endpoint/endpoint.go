// Package endpoint implements `iosuite endpoint deploy/list/destroy`.
//
// Manages remote provider endpoints (RunPod first; vast.ai / Modal
// later) so users can stand up the GPU side of the stack with one
// command. Round 2 of iosuite ships RunPod only.
//
// What `iosuite endpoint deploy --provider runpod --tool real-esrgan`
// does today:
//
//  1. Look up (or create) a RunPod template with the right image
//     and disk for the chosen tool. Image tag is pinned by iosuite's
//     own version — `iosuite endpoint deploy` always points at the
//     image that this version was tested with.
//  2. Look up (or create) a serverless endpoint named after `--name`
//     (default: `<tool>-<gpu-class>`) wired to that template, on the
//     GPU pool matching `--gpu-class`.
//  3. Print the endpoint id + ready-to-paste env line.
//
// Round 3 will add the smoke-test (cold start measurement, warm
// latency probe) that real-esrgan-serve's build/runpod_deploy.py
// already does — moving that here keeps the deploy story coherent
// for future tools without forking the python script per tool.
package endpoint

import (
	"context"
	"fmt"
	"io"
	"strings"

	"iosuite.io/internal/runpod"
)

// Provider names. Round 2 implements RunPod only; the constants
// exist so the cobra layer can validate `--provider` against a
// closed set today.
const (
	ProviderRunPod = "runpod"
)

// DeployInput captures everything `iosuite endpoint deploy` needs.
type DeployInput struct {
	Provider     string // ProviderRunPod
	Tool         string // "real-esrgan" (round 2 only)
	GPUClass     string // "rtx-4090" etc; mapped to a RunPod pool
	Name         string // endpoint name; auto-derived if empty
	APIKey       string // RunPod API key
	WorkersMax   int    // 0 → tool default
	IdleTimeoutS int    // 0 → tool default
	// Flashboot enables RunPod FlashBoot snapshot resume on the
	// endpoint. Pointer-typed so the cobra layer can distinguish
	// "user didn't pass --flashboot" (nil → use the tool default)
	// from an explicit --flashboot=false override.
	Flashboot *bool
	UserAgent string // surfaced to RunPod logs; iosuite/<version>
}

// DeployResult carries the outputs of a successful deploy.
type DeployResult struct {
	EndpointID   string
	EndpointName string
	TemplateID   string
	Image        string
	GPUPool      string
	Flashboot    bool
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
	tool, ok := Tools[in.Tool]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q. Known: %s", in.Tool, strings.Join(toolNames(), ", "))
	}
	pool, ok := GPUPools[in.GPUClass]
	if !ok {
		return nil, fmt.Errorf("unknown gpu-class %q. Known: %s", in.GPUClass, strings.Join(gpuClassNames(), ", "))
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

	// Template — find or save.
	existingTmpl, err := rp.FindTemplate(ctx, templateName)
	if err != nil {
		return nil, fmt.Errorf("look up template: %w", err)
	}
	tmplInput := runpod.SaveTemplateInput{
		Name:            templateName,
		Image:           tool.Image,
		ContainerDiskGB: tool.ContainerDiskGB,
	}
	if existingTmpl != nil {
		tmplInput.ExistingID = existingTmpl.ID
	}
	templateID, err := rp.SaveTemplate(ctx, tmplInput)
	if err != nil {
		return nil, fmt.Errorf("save template: %w", err)
	}

	// Endpoint — find or save.
	existing, err := rp.FindEndpoint(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("look up endpoint: %w", err)
	}
	flashboot := tool.Flashboot
	if in.Flashboot != nil {
		flashboot = *in.Flashboot
	}
	epInput := runpod.SaveEndpointInput{
		Name:         name,
		TemplateID:   templateID,
		GPUPool:      pool,
		WorkersMin:   0,
		WorkersMax:   defaultIfZero(in.WorkersMax, tool.WorkersMax),
		IdleTimeoutS: defaultIfZero(in.IdleTimeoutS, tool.IdleTimeoutS),
		Flashboot:    flashboot,
	}
	if existing != nil {
		epInput.ExistingID = existing.ID
	}
	endpointID, err := rp.SaveEndpoint(ctx, epInput)
	if err != nil {
		return nil, fmt.Errorf("save endpoint: %w", err)
	}

	return &DeployResult{
		EndpointID:   endpointID,
		EndpointName: name,
		TemplateID:   templateID,
		Image:        tool.Image,
		GPUPool:      pool,
		Flashboot:    flashboot,
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

func toolNames() []string {
	out := make([]string, 0, len(Tools))
	for k := range Tools {
		out = append(out, k)
	}
	return out
}

func gpuClassNames() []string {
	out := make([]string, 0, len(GPUPools))
	for k := range GPUPools {
		out = append(out, k)
	}
	return out
}
