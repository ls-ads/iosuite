package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var (
	stopYes     bool
	stopVolumes bool
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop running processes or tear down cloud resources",
	Long:  "Stops active processing or destroys cloud infrastructure based on the selected model and provider.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if provider == "" || model == "" {
			return fmt.Errorf("required flag(s) \"provider\" and \"model\" not set")
		}
		p := iocore.UpscaleProvider(provider)
		ctx := context.Background()

		// If provider is runpod, clean up RunPod resources
		if p == iocore.ProviderRunPod {
			return runStopRunPod(ctx)
		}

		// If model is ffmpeg (and provider is local), stop local ffmpeg
		if model == "ffmpeg" {
			return iocore.CleanupLocalFFmpeg(ctx)
		}

		fmt.Printf("No stop action defined for model '%s' on provider '%s'\n", model, p)
		return nil
	},
}

func runStopRunPod(ctx context.Context) error {
	key := apiKey
	if key == "" {
		key = os.Getenv("RUNPOD_API_KEY")
	}
	if key == "" {
		return fmt.Errorf("API key is required for RunPod stop (set via -k or RUNPOD_API_KEY)")
	}

	endpointName := iocore.GetRunPodEndpointName(model)

	fmt.Printf("Searching for RunPod endpoints with prefix '%s'...\n", endpointName)

	endpoints, err := iocore.GetRunPodEndpoints(ctx, key, endpointName)
	if err != nil {
		return fmt.Errorf("failed to get RunPod endpoints: %v", err)
	}

	if len(endpoints) == 0 {
		fmt.Println("No matching RunPod endpoints found.")
		return nil
	}

	fmt.Printf("Found %d endpoint(s) to delete:\n", len(endpoints))
	for _, e := range endpoints {
		fmt.Printf(" - ID: %s, Name: %s\n", e.ID, e.Name)
	}

	if !stopYes {
		fmt.Print("Are you sure you want to delete these endpoints? (y/N): ")
		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			fmt.Println("Stop aborted.")
			return nil
		}
	}

	deletedCount := 0
	for _, e := range endpoints {
		fmt.Printf("Deleting endpoint %s (%s)...\n", e.ID, e.Name)
		err := iocore.DeleteRunPodEndpoint(ctx, key, e.ID)
		if err != nil {
			fmt.Printf("Failed to delete %s: %v\n", e.ID, err)
		} else {
			deletedCount++
		}
	}

	fmt.Printf("Successfully deleted %d RunPod endpoint(s).\n", deletedCount)

	if stopVolumes {
		volumeSet := make(map[string]bool)
		for _, e := range endpoints {
			if e.NetworkVolumeID != "" {
				volumeSet[e.NetworkVolumeID] = true
			}
			for _, vID := range e.NetworkVolumeIDs {
				if vID != "" {
					volumeSet[vID] = true
				}
			}
		}

		if len(volumeSet) == 0 {
			fmt.Println("No attached RunPod network volumes found to delete.")
		} else {
			fmt.Printf("\nFound %d attached network volume(s) to delete:\n", len(volumeSet))
			for vID := range volumeSet {
				fmt.Printf(" - Volume ID: %s\n", vID)
			}

			if !stopYes {
				fmt.Print("Are you sure you want to delete these volumes? All data will be lost. (y/N): ")
				var vResponse string
				fmt.Scanln(&vResponse)
				vResponse = strings.ToLower(strings.TrimSpace(vResponse))
				if vResponse != "y" && vResponse != "yes" {
					fmt.Println("Volume deletion aborted.")
					return nil
				}
			}

			vDeletedCount := 0
			for vID := range volumeSet {
				fmt.Printf("Deleting volume %s...\n", vID)
				err := iocore.DeleteNetworkVolume(ctx, key, vID)
				if err != nil {
					fmt.Printf("Failed to delete volume %s: %v\n", vID, err)
				} else {
					vDeletedCount++
				}
			}
			fmt.Printf("Successfully deleted %d network volume(s).\n", vDeletedCount)
		}
	}

	return nil
}

var stopRunPodCmd = &cobra.Command{
	Use:    "runpod",
	Short:  "Tear down created runpod endpoints (legacy)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStopRunPod(context.Background())
	},
}

var stopFFmpegCmd = &cobra.Command{
	Use:    "ffmpeg",
	Short:  "Stop local FFmpeg binaries and clean up temporary files (legacy)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		return iocore.CleanupLocalFFmpeg(ctx)
	},
}

func init() {
	stopCmd.Flags().BoolVarP(&stopYes, "yes", "y", false, "Skip confirmation prompt")
	stopCmd.Flags().BoolVar(&stopVolumes, "volumes", false, "Also delete any attached network volumes")

	stopCmd.AddCommand(stopRunPodCmd)
	stopCmd.AddCommand(stopFFmpegCmd)
	rootCmd.AddCommand(stopCmd)
}
