package iocore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/runpod/go-sdk/pkg/sdk"
	"github.com/runpod/go-sdk/pkg/sdk/config"
	rpEndpoint "github.com/runpod/go-sdk/pkg/sdk/endpoint"
)

// NetworkVolume represents a RunPod network volume.
type NetworkVolume struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Size         int    `json:"size"`
	DataCenterID string `json:"dataCenterId"`
	Status       string `json:"status"`
}

// RunPodAvailableGPUs is a list of all valid RunPod GPU types.
var RunPodAvailableGPUs = []string{
	"NVIDIA GeForce RTX 4090", "NVIDIA A40", "NVIDIA RTX A5000", "NVIDIA GeForce RTX 5090",
	"NVIDIA H100 80GB HBM3", "NVIDIA GeForce RTX 3090", "NVIDIA RTX A4500", "NVIDIA L40S",
	"NVIDIA H200", "NVIDIA L4", "NVIDIA RTX 6000 Ada Generation", "NVIDIA A100-SXM4-80GB",
	"NVIDIA RTX 4000 Ada Generation", "NVIDIA RTX A6000", "NVIDIA A100 80GB PCIe",
	"NVIDIA RTX 2000 Ada Generation", "NVIDIA RTX A4000", "NVIDIA RTX PRO 6000 Blackwell Server Edition",
	"NVIDIA H100 PCIe", "NVIDIA H100 NVL", "NVIDIA L40", "NVIDIA B200",
	"NVIDIA GeForce RTX 3080 Ti", "NVIDIA RTX PRO 6000 Blackwell Workstation Edition",
	"NVIDIA GeForce RTX 3080", "NVIDIA GeForce RTX 3070", "AMD Instinct MI300X OAM",
	"NVIDIA GeForce RTX 4080 SUPER", "Tesla V100-PCIE-16GB", "Tesla V100-SXM2-32GB",
	"NVIDIA RTX 5000 Ada Generation", "NVIDIA GeForce RTX 4070 Ti", "NVIDIA RTX 4000 SFF Ada Generation",
	"NVIDIA GeForce RTX 3090 Ti", "NVIDIA RTX A2000", "NVIDIA GeForce RTX 4080", "NVIDIA A30",
	"NVIDIA GeForce RTX 5080", "Tesla V100-FHHL-16GB", "NVIDIA H200 NVL", "Tesla V100-SXM2-16GB",
	"NVIDIA RTX PRO 6000 Blackwell Max-Q Workstation Edition", "NVIDIA A5000 Ada", "Tesla V100-PCIE-32GB",
	"NVIDIA RTX A4500", "NVIDIA A30", "NVIDIA GeForce RTX 3080TI", "Tesla T4", "NVIDIA RTX A30",
}

// RunPodEndpointConfig holds configuration for auto-provisioning a RunPod Serverless Endpoint.
type RunPodEndpointConfig struct {
	Name             string   `json:"name"`
	TemplateID       string   `json:"templateId"`
	GPUTypeIDs       []string `json:"gpuTypeIds,omitempty"`
	GPUCount         int      `json:"gpuCount,omitempty"`
	DataCenterIDs    []string `json:"dataCenterIds,omitempty"`
	WorkersMin       int      `json:"workersMin"`
	WorkersMax       int      `json:"workersMax"`
	IdleTimeout      int      `json:"idleTimeout"`
	Flashboot        bool     `json:"flashboot"`
	NetworkVolumeID  string   `json:"networkVolumeId,omitempty"`
	NetworkVolumeIDs []string `json:"networkVolumeIds,omitempty"`
	ComputeType      string   `json:"computeType,omitempty"`
}

// EnsureRunPodEndpoint checks if a RunPod endpoint with the given name prefix exists.
// If it does, it returns the endpoint ID. Otherwise, it creates a new endpoint
// using the provided config and returns its ID.
func EnsureRunPodEndpoint(ctx context.Context, key string, config RunPodEndpointConfig) (string, error) {
	// 1. Check if endpoint exists via REST API
	listURL := "https://rest.runpod.io/v1/endpoints"
	listReq, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create list endpoints request: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{}
	listResp, err := client.Do(listReq)
	if err != nil {
		return "", fmt.Errorf("failed to list RunPod endpoints: %v", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode == http.StatusOK {
		var endpoints []RunPodEndpoint
		if err := json.NewDecoder(listResp.Body).Decode(&endpoints); err == nil {
			for _, e := range endpoints {
				if strings.HasPrefix(e.Name, config.Name) {
					Debug("Using existing RunPod endpoint", "id", e.ID, "matched_name", e.Name)
					return e.ID, nil
				}
			}
		}
	} else {
		body, _ := io.ReadAll(listResp.Body)
		Debug("Failed to list RunPod endpoints", "status", listResp.StatusCode, "body", string(body))
	}

	Debug("RunPod endpoint not found, creating", "name", config.Name)

	createURL := "https://rest.runpod.io/v1/endpoints"
	jsonData, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal create endpoint request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", createURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request for RunPod endpoint creation: %v", err)
	}
	// RunPod REST API uses Bearer authentication
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to perform RunPod endpoint creation request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("RunPod API returned status %d when creating endpoint: %s", resp.StatusCode, string(body))
	}

	var createData RunPodEndpoint

	if err := json.NewDecoder(resp.Body).Decode(&createData); err != nil {
		return "", fmt.Errorf("failed to parse RunPod endpoint creation response: %v", err)
	}

	Debug("Created new RunPod endpoint", "id", createData.ID, "name", createData.Name)
	return createData.ID, nil
}

// RunPodJobResponse represents the response from a RunPod serverless job.
type RunPodJobResponse struct {
	ID            string                 `json:"id"`
	Status        string                 `json:"status"`
	DelayTime     int64                  `json:"delayTime"`     // queue delay in milliseconds
	ExecutionTime int64                  `json:"executionTime"` // in milliseconds
	Output        map[string]interface{} `json:"output"`
	Error         string                 `json:"error"`
}

// NewRunPodEndpointClient creates a RunPod Go SDK endpoint client for the given API key and endpoint ID.
func NewRunPodEndpointClient(apiKey, endpointID string) (*rpEndpoint.Endpoint, error) {
	return rpEndpoint.New(
		&config.Config{ApiKey: &apiKey},
		&rpEndpoint.Option{EndpointId: &endpointID},
	)
}

// RunRunPodJobSync submits a job to a RunPod endpoint using the Go SDK's RunSync method,
// which blocks server-side until the job completes. This eliminates polling latency
// entirely — the result is returned the instant the job finishes.
func RunRunPodJobSync(ctx context.Context, key, endpointID string, input map[string]interface{}, statusCallback func(phase, message string, elapsed time.Duration)) (*RunPodJobResponse, error) {
	ep, err := NewRunPodEndpointClient(key, endpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to create RunPod endpoint client: %v", err)
	}

	start := time.Now()
	if statusCallback != nil {
		statusCallback("queued", "Submitted job, waiting for result...", 0)
	}

	// RunSync blocks until the job completes (up to timeout seconds).
	// 300s timeout to handle cold starts which can take minutes.
	output, err := ep.RunSync(&rpEndpoint.RunSyncInput{
		JobInput: &rpEndpoint.JobInput{
			Input: input,
		},
		Timeout: sdk.Int(300),
	})
	elapsed := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("runsync request failed: %v", err)
	}

	// Check for SDK-level errors
	if output.Error != nil && *output.Error != "" {
		return nil, fmt.Errorf("runpod job failed: %s", *output.Error)
	}

	// Build our typed response from the SDK's generic output
	job := &RunPodJobResponse{}
	if output.Id != nil {
		job.ID = *output.Id
	}
	if output.Status != nil {
		job.Status = *output.Status
	}
	if output.DelayTime != nil {
		job.DelayTime = int64(*output.DelayTime)
	}
	if output.ExecutionTime != nil {
		job.ExecutionTime = int64(*output.ExecutionTime)
	}

	// Parse the generic output into our typed struct.
	// The SDK returns Output as *interface{}, so we marshal it back to JSON
	// then decode into our known output shape.
	if output.Output != nil {
		outputJSON, err := json.Marshal(*output.Output)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal SDK output: %v", err)
		}
		if err := json.Unmarshal(outputJSON, &job.Output); err != nil {
			return nil, fmt.Errorf("failed to parse job output: %v, raw: %s", err, string(outputJSON))
		}
	}

	switch job.Status {
	case "COMPLETED":
		if statusCallback != nil {
			statusCallback("completed", "Processing complete", elapsed)
		}
		return job, nil
	case "FAILED":
		errMsg := job.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, fmt.Errorf("runpod job failed: %s", errMsg)
	default:
		return nil, fmt.Errorf("runsync returned unexpected status: %s", job.Status)
	}
}

// GetRunPodJobStatus checks the status of a RunPod job using the Go SDK.
func GetRunPodJobStatus(key, endpointID, jobID string) (*RunPodJobResponse, error) {
	ep, err := NewRunPodEndpointClient(key, endpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to create RunPod endpoint client: %v", err)
	}

	output, err := ep.Status(&rpEndpoint.StatusInput{
		Id:             sdk.String(jobID),
		RequestTimeout: sdk.Int(10),
	})
	if err != nil {
		return nil, fmt.Errorf("status request failed: %v", err)
	}

	job := &RunPodJobResponse{}
	if output.Id != nil {
		job.ID = *output.Id
	}
	if output.Status != nil {
		job.Status = *output.Status
	}
	if output.DelayTime != nil {
		job.DelayTime = int64(*output.DelayTime)
	}
	if output.ExecutionTime != nil {
		job.ExecutionTime = int64(*output.ExecutionTime)
	}
	if output.Error != nil {
		job.Error = *output.Error
	}

	if output.Output != nil {
		outputJSON, err := json.Marshal(*output.Output)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal SDK output: %v", err)
		}
		if err := json.Unmarshal(outputJSON, &job.Output); err != nil {
			return nil, fmt.Errorf("failed to parse job output: %v", err)
		}
	}

	return job, nil
}

// CancelRunPodJob cancels a RunPod job using the Go SDK.
func CancelRunPodJob(key, endpointID, jobID string) error {
	ep, err := NewRunPodEndpointClient(key, endpointID)
	if err != nil {
		return fmt.Errorf("failed to create RunPod endpoint client: %v", err)
	}

	_, err = ep.Cancel(&rpEndpoint.CancelInput{
		Id: sdk.String(jobID),
	})
	return err
}

// GetRunPodEndpointHealth retrieves health information for a RunPod endpoint using the Go SDK.
func GetRunPodEndpointHealth(key, endpointID string) (*rpEndpoint.HealthOutput, error) {
	ep, err := NewRunPodEndpointClient(key, endpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to create RunPod endpoint client: %v", err)
	}

	return ep.Health(&rpEndpoint.HealthInput{
		RequestTimeout: sdk.Int(10),
	})
}

func GetRunPodEndpointName(model string) string {
	if model == "ffmpeg" {
		return "iosuite-ffmpeg"
	}
	if model == "real-esrgan" || model == "" {
		return "iosuite-img-real-esrgan"
	}
	return "iosuite-img-" + model
}

// ModelConfig holds the cloud configuration for a specific model.
type ModelConfig struct {
	TemplateID      string
	GPUIDs          []string
	NetworkVolumeID string
}

// ProvisionRunPodModel handles the end-to-end provisioning of a RunPod endpoint for a model.
func ProvisionRunPodModel(ctx context.Context, key string, model string, modelCfg ModelConfig, dataCenterIDs []string, workersMin int) (string, error) {
	endpointName := GetRunPodEndpointName(model)

	// 1. Check if an endpoint for this model already exists
	existing, err := GetRunPodEndpoints(ctx, key, endpointName)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing endpoints: %v", err)
	}
	if len(existing) > 0 {
		return existing[0].ID, nil
	}

	// 2. Provision new endpoint
	endpointID, err := EnsureRunPodEndpoint(ctx, key, RunPodEndpointConfig{
		Name:            endpointName,
		TemplateID:      modelCfg.TemplateID,
		GPUTypeIDs:      modelCfg.GPUIDs,
		DataCenterIDs:   dataCenterIDs,
		WorkersMin:      workersMin,
		WorkersMax:      1,
		IdleTimeout:     5,
		Flashboot:       true,
		NetworkVolumeID: modelCfg.NetworkVolumeID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to provision RunPod endpoint: %v", err)
	}

	return endpointID, nil
}

type RunPodEndpoint struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	GPUTypeIDs       []string `json:"gpuTypeIds"`
	WorkersMin       int      `json:"workersMin"`
	NetworkVolumeID  string   `json:"networkVolumeId"`
	NetworkVolumeIDs []string `json:"networkVolumeIds"`
}

// CalculateRunPodEndpointRate calculates the rate per second according to the endpoint's GPU and scaling profile.
func CalculateRunPodEndpointRate(gpuTypeIds []string, workersMin int) float64 {
	if len(gpuTypeIds) == 0 {
		return 0.00019 // default fallback
	}

	// Normalize spaces in case of irregular options like "NVIDIA  RTX A4500"
	gpuType := strings.Join(strings.Fields(gpuTypeIds[0]), " ")
	isActive := workersMin > 0

	switch gpuType {
	case "NVIDIA B200":
		if isActive {
			return 0.00190
		}
		return 0.00240
	case "NVIDIA H200", "NVIDIA H200 NVL":
		if isActive {
			return 0.00124
		}
		return 0.00155
	case "NVIDIA H100 80GB HBM3", "NVIDIA H100 PCIe", "NVIDIA H100 NVL":
		if isActive {
			return 0.00093
		}
		return 0.00116
	case "NVIDIA A100-SXM4-80GB", "NVIDIA A100 80GB PCIe":
		if isActive {
			return 0.00060
		}
		return 0.00076
	case "NVIDIA L40", "NVIDIA L40S", "NVIDIA RTX 6000 Ada Generation":
		if isActive {
			return 0.00037
		}
		return 0.00053
	case "NVIDIA RTX A6000", "NVIDIA A40", "NVIDIA GeForce RTX 5090":
		if isActive {
			return 0.00031
		}
		return 0.00044
	case "NVIDIA GeForce RTX 4090":
		if isActive {
			return 0.00021
		}
		return 0.00031
	case "NVIDIA L4", "NVIDIA RTX A5000", "NVIDIA A5000 Ada", "NVIDIA GeForce RTX 3090", "NVIDIA GeForce RTX 3090 Ti":
		if isActive {
			return 0.00013
		}
		return 0.00019
	case "NVIDIA RTX A4000", "NVIDIA RTX A4500", "NVIDIA RTX 4000 Ada Generation", "NVIDIA RTX 4000 SFF Ada Generation", "NVIDIA RTX 2000 Ada Generation", "NVIDIA RTX A2000":
		if isActive {
			return 0.00011
		}
		return 0.00016
	default:
		if isActive {
			return 0.00013
		}
		return 0.00019
	}
}

// GetRunPodEndpoints gets all RunPod serverless endpoints that match the given name prefix.
func GetRunPodEndpoints(ctx context.Context, key, namePrefix string) ([]RunPodEndpoint, error) {
	listURL := "https://rest.runpod.io/v1/endpoints"
	listReq, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list endpoints request: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{}
	listResp, err := client.Do(listReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list RunPod endpoints: %v", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		return nil, fmt.Errorf("RunPod API returned status %d when listing endpoints: %s", listResp.StatusCode, string(body))
	}

	var allEndpoints []RunPodEndpoint
	if err := json.NewDecoder(listResp.Body).Decode(&allEndpoints); err != nil {
		return nil, fmt.Errorf("failed to parse endpoints list: %v", err)
	}

	var matched []RunPodEndpoint
	for _, e := range allEndpoints {
		if strings.HasPrefix(e.Name, namePrefix) {
			matched = append(matched, e)
		}
	}

	return matched, nil
}

// DeleteRunPodEndpoint deletes a specific RunPod serverless endpoint by ID.
func DeleteRunPodEndpoint(ctx context.Context, key, id string) error {
	deleteURL := fmt.Sprintf("https://rest.runpod.io/v1/endpoints/%s", id)
	delReq, err := http.NewRequestWithContext(ctx, "DELETE", deleteURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request for endpoint: %v", err)
	}
	delReq.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{}
	delResp, err := client.Do(delReq)
	if err != nil {
		return fmt.Errorf("failed to delete endpoint: %v", err)
	}
	defer delResp.Body.Close()

	if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(delResp.Body)
		return fmt.Errorf("failed to delete endpoint (bad status): %d, body: %s", delResp.StatusCode, string(body))
	}

	return nil
}

// CreateNetworkVolume creates a new network volume on RunPod.
func CreateNetworkVolume(ctx context.Context, key, name string, sizeGB int, dataCenterID string) (string, error) {
	url := "https://rest.runpod.io/v1/networkvolumes"
	payload := map[string]interface{}{
		"name":         name,
		"size":         sizeGB,
		"dataCenterId": dataCenterID,
	}
	if sizeGB < 10 {
		return "", fmt.Errorf("RunPod network volume size must be at least 10GB")
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to create network volume: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.ID, nil
}

// DeleteNetworkVolume deletes a RunPod network volume by ID.
func DeleteNetworkVolume(ctx context.Context, key, id string) error {
	url := fmt.Sprintf("https://rest.runpod.io/v1/networkvolumes/%s", id)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete network volume: %d - %s", resp.StatusCode, string(body))
	}

	return nil
}

// ListNetworkVolumes lists all network volumes for the account.
func ListNetworkVolumes(ctx context.Context, key string) ([]NetworkVolume, error) {
	url := "https://rest.runpod.io/v1/networkvolumes"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list network volumes: %d - %s", resp.StatusCode, string(body))
	}

	var volumes []NetworkVolume
	if err := json.NewDecoder(resp.Body).Decode(&volumes); err != nil {
		return nil, err
	}

	return volumes, nil
}

// GetS3Endpoint returns the S3-compatible API endpoint for a specific region.
func GetS3Endpoint(region string) string {
	regionClean := strings.ToLower(strings.ReplaceAll(region, "_", "-"))
	return fmt.Sprintf("https://s3api-%s.runpod.io/", regionClean)
}

// VolumeWorkflowConfig holds configuration for the high-level serverless volume workflow.
type VolumeWorkflowConfig struct {
	APIKey         string
	Region         string
	EndpointID     string
	VolumeSizeGB   int
	VolumeID       string // Optional: if provided, uses existing volume
	UseVolume      bool   // Indicator to use network volume (triggers auto-discovery if VolumeID is empty)
	InputLocalPath string
	OutputLocalDir string
	TemplateID     string   // For provisioning
	GPUID          string   // For provisioning
	FFmpegArgs     string   // For ffmpeg model
	OutputExt      string   // For ffmpeg model
	DataCenterIDs  []string // For provisioning
	KeepFailed     bool
}

// RunPodServerlessVolumeWorkflow handles the full lifecycle: volume -> upload -> serverless job -> download -> cleanup.
func RunPodServerlessVolumeWorkflow(ctx context.Context, cfg VolumeWorkflowConfig, status func(phase, message string)) error {
	key := cfg.APIKey
	if key == "" {
		key = os.Getenv("RUNPOD_API_KEY")
	}

	// 1. Resolve/Auto-discover VolumeID
	volumeID := cfg.VolumeID
	endpointID := cfg.EndpointID
	useVolume := cfg.UseVolume || volumeID != "" || cfg.VolumeSizeGB >= 10

	// If no volume ID provided but volume workflow requested, try to find it from the endpoint configuration
	if volumeID == "" && endpointID != "" && useVolume {
		status("infrastructure", "Discovering attached network volume...")
		endpoints, err := GetRunPodEndpoints(ctx, key, "")
		if err == nil {
			for _, e := range endpoints {
				if e.ID == endpointID {
					if e.NetworkVolumeID != "" {
						volumeID = e.NetworkVolumeID
						status("infrastructure", fmt.Sprintf("Auto-discovered volume: %s", volumeID))
					} else if len(e.NetworkVolumeIDs) > 0 {
						volumeID = e.NetworkVolumeIDs[0]
						status("infrastructure", fmt.Sprintf("Auto-discovered volume: %s", volumeID))
					}
					break
				}
			}
		}
	}

	// 2. Create Volume if still missing and a size was requested
	if volumeID == "" && cfg.VolumeSizeGB >= 10 {
		status("infrastructure", "Creating network volume...")
		vid, err := CreateNetworkVolume(ctx, key, fmt.Sprintf("io-vol-%d", time.Now().Unix()), cfg.VolumeSizeGB, cfg.Region)
		if err != nil {
			return fmt.Errorf("failed to create volume: %v", err)
		}
		volumeID = vid
		status("infrastructure", fmt.Sprintf("Created volume: %s", volumeID))

		// Settle time for RunPod volume to be ready for S3
		time.Sleep(5 * time.Second)
	}

	// 3. Resolve Region from Volume (critical for S3 307 redirects)
	region := cfg.Region
	if volumeID != "" {
		vols, err := ListNetworkVolumes(ctx, key)
		if err == nil {
			for _, v := range vols {
				if v.ID == volumeID {
					if v.DataCenterID != "" {
						region = v.DataCenterID
						status("infrastructure", fmt.Sprintf("Resolved volume region: %s", region))
					}
					break
				}
			}
		}
	}

	// 4. Setup S3 Client
	s3Access := os.Getenv("AWS_ACCESS_KEY_ID")
	s3Secret := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if s3Access == "" || s3Secret == "" {
		return fmt.Errorf("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are strictly required for Network Volume access")
	}

	s3Client, err := NewS3Client(ctx, region, s3Access, s3Secret, volumeID)
	if err != nil {
		return fmt.Errorf("failed to setup S3 client: %v", err)
	}

	// 3. Upload Input
	inputFileName := filepath.Base(cfg.InputLocalPath)
	status("upload", fmt.Sprintf("Uploading %s to volume...", inputFileName))
	if err := s3Client.UploadFile(ctx, cfg.InputLocalPath, inputFileName); err != nil {
		return fmt.Errorf("upload failed: %v", err)
	}

	// 4. Ensure Endpoint exists (if we have provisioning info)
	if endpointID == "" && cfg.TemplateID != "" {
		status("infrastructure", "Provisioning serverless endpoint...")
		modelCfg := ModelConfig{
			TemplateID:      cfg.TemplateID,
			GPUIDs:          []string{cfg.GPUID},
			NetworkVolumeID: volumeID,
		}
		eid, err := ProvisionRunPodModel(ctx, key, "workflow", modelCfg, cfg.DataCenterIDs, 0)
		if err != nil {
			return fmt.Errorf("failed to provision endpoint: %v", err)
		}
		endpointID = eid
	}

	if endpointID == "" {
		return fmt.Errorf("endpoint ID is required for serverless workflow")
	}

	// 5. Submit Serverless Job
	outputFileName := "out_" + inputFileName
	if cfg.OutputExt != "" {
		ext := filepath.Ext(outputFileName)
		outputFileName = strings.TrimSuffix(outputFileName, ext) + "." + cfg.OutputExt
	}

	status("processing", "Submitting serverless job...")

	input := buildVolumeJobInput(cfg.EndpointID, cfg.TemplateID, inputFileName, outputFileName, cfg.FFmpegArgs, cfg.OutputExt)

	job, err := RunRunPodJobSync(ctx, key, endpointID, input, func(phase, message string, elapsed time.Duration) {
		status(phase, message)
	})
	if err != nil {
		return fmt.Errorf("serverless job failed: %v", err)
	}

	// 6. Download Output
	status("download", "Downloading result from volume...")
	downloadPath := filepath.Join(cfg.OutputLocalDir, outputFileName)

	// If the job returned a specific output_path, use that
	remoteOut := outputFileName
	if outPath, ok := job.Output["output_path"].(string); ok && outPath != "" {
		remoteOut = outPath
	}

	// S3 keys must not have the mount prefix
	s3Key := strings.TrimPrefix(remoteOut, runpodVolumeMount+"/")

	if err := s3Client.DownloadFile(ctx, s3Key, downloadPath); err != nil {
		return fmt.Errorf("download failed: %v", err)
	}

	// 7. Cleanup (Optional)
	if !cfg.KeepFailed {
		status("cleanup", "Cleaning up network volume...")
		_ = DeleteNetworkVolume(ctx, key, volumeID)
	}

	return nil
}

const runpodVolumeMount = "/runpod-volume"

func buildVolumeJobInput(endpointID, templateID, inputFileName, outputFileName, ffmpegArgs, outputExt string) map[string]interface{} {
	input := map[string]interface{}{}

	// Remote paths within the worker must be prefixed with /runpod-volume
	remoteInput := filepath.Join(runpodVolumeMount, inputFileName)
	remoteOutput := filepath.Join(runpodVolumeMount, outputFileName)

	// Check if this is for ffmpeg or real-esrgan (image vs media)
	if strings.Contains(endpointID, "img") || templateID == "047z8w5i69" {
		input["image_path"] = remoteInput
		input["output_path"] = remoteOutput
		if outputExt != "" {
			input["output_format"] = outputExt
		}
	} else {
		input["input_path"] = remoteInput
		input["output_path"] = remoteOutput
		if ffmpegArgs != "" {
			input["ffmpeg_args"] = ffmpegArgs
		}
	}
	return input
}
