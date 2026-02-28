package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Initialize and provision cloud infrastructure for the selected model",
	RunE: func(cmd *cobra.Command, args []string) error {
		if provider == "" || model == "" {
			return fmt.Errorf("required flag(s) \"provider\" and \"model\" not set")
		}
		providerTyped := iocore.UpscaleProvider(provider)

		if providerTyped != iocore.ProviderRunPod {
			fmt.Printf("Starting infrastructure is not required for provider: %s\n", providerTyped)
			return nil
		}

		key := apiKey
		if key == "" {
			key = os.Getenv("RUNPOD_API_KEY")
		}
		if key == "" {
			return fmt.Errorf("api key is required for runpod start (set via -k or RUNPOD_API_KEY)")
		}

		ctx := context.Background()

		workersMin := 0
		if activeWorkers {
			workersMin = 1
		}

		// Use dataCenterIds slice directly from flags

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
				TemplateID: "uduo7jdyhn",
				GPUIDs:     gpuIDs,
			}
		} else if model == "real-esrgan" {
			modelCfg = iocore.ModelConfig{
				TemplateID: "047z8w5i69",
				GPUIDs:     gpuIDs,
			}
		} else {
			return fmt.Errorf("unsupported model for RunPod infrastructure: %s (supported: ffmpeg, real-esrgan)", model)
		}

		fmt.Printf("Initializing RunPod infrastructure for model '%s'...\n", model)
		if activeWorkers {
			fmt.Println("Mode: always active (workersMin=1)")
		}
		fmt.Println("This may take 10+ minutes depending on template size and GPU availability.")

		if volumeSize > 0 {
			fmt.Printf("Provisioning RunPod Network Volume (%d GB) in %s...\n", volumeSize, dataCenterIds[0])
			volumeID, err := iocore.CreateNetworkVolume(ctx, key, fmt.Sprintf("io-vol-%s-%d", model, time.Now().Unix()), volumeSize, dataCenterIds[0])
			if err != nil {
				return fmt.Errorf("failed to create volume: %v", err)
			}
			fmt.Printf("Successfully created RunPod volume!\nVolume ID: %s\n", volumeID)
			return nil
		}

		endpointID, err := iocore.ProvisionRunPodModel(ctx, key, model, modelCfg, dataCenterIds, workersMin)
		if err != nil {
			return fmt.Errorf("failed to initialize infrastructure: %v", err)
		}

		fmt.Printf("Successfully initialized RunPod endpoint!\nEndpoint ID: %s\n", endpointID)
		return nil
	},
}

func init() {
	startCmd.Flags().BoolVar(&activeWorkers, "active", false, "Set endpoint to always active (workersMin=1)")
	startCmd.Flags().StringSliceVar(&dataCenterIds, "data-center", []string{"EU-RO-1"}, "Direct RunPod data center IDs")
	startCmd.Flags().StringVar(&gpuType, "gpu", "", "Specific GPU type for RunPod (e.g. 'NVIDIA RTX A4000')")
	startCmd.Flags().IntVar(&volumeSize, "volume-size", 0, "Provision a network volume of specified size in GB instead of an endpoint")
	startCmd.Flags().BoolVar(&keepFailed, "keep-failed", false, "Keep resources on failure (for debugging)")

	rootCmd.AddCommand(startCmd)
}
