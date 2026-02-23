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
	cleanupAPIKey string
	cleanupYes    bool
	cleanupModel  string
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Tear down created endpoints and resources",
	Long:  "Commands to selectively tear down and clean up infrastructure resources.",
}

var cleanupRunPodCmd = &cobra.Command{
	Use:   "runpod",
	Short: "Tear down created runpod endpoints",
	RunE: func(cmd *cobra.Command, args []string) error {
		key := cleanupAPIKey
		if key == "" {
			key = os.Getenv("RUNPOD_API_KEY")
		}
		if key == "" {
			return fmt.Errorf("API key is required for RunPod cleanup (set via -k or RUNPOD_API_KEY)")
		}

		ctx := context.Background()
		endpointName := iocore.GetRunPodEndpointName(cleanupModel)
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

		if !cleanupYes {
			fmt.Print("Are you sure you want to delete these endpoints? (y/N): ")
			var response string
			fmt.Scanln(&response)
			response = strings.ToLower(strings.TrimSpace(response))
			if response != "y" && response != "yes" {
				fmt.Println("Cleanup aborted.")
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
		return nil
	},
}

func init() {
	// Root cleanup command flags (if any apply to all)
	cleanupRunPodCmd.Flags().StringVarP(&cleanupAPIKey, "api-key", "k", "", "API Key for RunPod")
	cleanupRunPodCmd.Flags().StringVarP(&cleanupModel, "model", "m", "real-esrgan", "Model or Endpoint ID to cleanup")
	cleanupRunPodCmd.Flags().BoolVarP(&cleanupYes, "yes", "y", false, "Skip confirmation prompt")

	cleanupCmd.AddCommand(cleanupRunPodCmd)
	rootCmd.AddCommand(cleanupCmd)
}
