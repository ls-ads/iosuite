package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var (
	activeWorkers bool
	region        string
	gpuType       string
	dataCenter    string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize and provision cloud infrastructure for the selected model",
	RunE: func(cmd *cobra.Command, args []string) error {
		providerTyped := iocore.UpscaleProvider(provider)

		if providerTyped != iocore.ProviderRunPod {
			fmt.Printf("Initialization is not required for provider: %s\n", providerTyped)
			return nil
		}

		key := apiKey
		if key == "" {
			key = os.Getenv("RUNPOD_API_KEY")
		}
		if key == "" {
			return fmt.Errorf("api key is required for runpod init (set via -k or RUNPOD_API_KEY)")
		}

		ctx := context.Background()

		workersMin := 0
		if activeWorkers {
			workersMin = 1
		}

		dataCenterIDs, err := iocore.RegionToDataCenterIDs(region)
		if err != nil {
			return err
		}
		if dataCenter != "" {
			dataCenterIDs = []string{dataCenter}
		}

		// GPU configuration
		gpuIDs := []string{
			"NVIDIA RTX A4000",
			"NVIDIA RTX A4500",
			"NVIDIA RTX 4000 Ada Generation",
			"NVIDIA RTX 4000 SFF Ada Generation",
			"NVIDIA RTX 2000 Ada Generation",
			"NVIDIA RTX A2000",
		}
		if gpuType != "" {
			gpuIDs = []string{gpuType}
		}

		// Model Configuration
		var modelCfg iocore.ModelConfig
		if model == "ffmpeg" {
			modelCfg = iocore.ModelConfig{
				TemplateID: "047z8w5i69",
				GPUIDs:     gpuIDs,
			}
		} else {
			// Default to upscaler config
			modelCfg = iocore.ModelConfig{
				TemplateID: "047z8w5i69",
				GPUIDs:     gpuIDs,
			}
		}

		fmt.Printf("Initializing RunPod infrastructure for model '%s'...\n", model)
		if activeWorkers {
			fmt.Println("Mode: always active (workersMin=1)")
		}
		fmt.Println("This may take 10+ minutes depending on template size and GPU availability.")

		endpointID, err := iocore.ProvisionRunPodModel(ctx, key, model, modelCfg, dataCenterIDs, workersMin)
		if err != nil {
			return fmt.Errorf("failed to initialize infrastructure: %v", err)
		}

		fmt.Printf("Successfully initialized RunPod endpoint!\nEndpoint ID: %s\n", endpointID)
		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&activeWorkers, "active", false, "Set endpoint to always active (workersMin=1)")
	initCmd.Flags().StringVar(&region, "region", "all", "Region for endpoint (us, eu, ca, all)")
	initCmd.Flags().StringVar(&gpuType, "gpu", "", "Specific GPU type for RunPod (e.g. 'NVIDIA RTX A4000')")
	initCmd.Flags().StringVar(&dataCenter, "datacenter", "EU-RO-1", "Direct RunPod datacenter ID (overrides region)")

	rootCmd.AddCommand(initCmd)
}
