package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var (
	volName string
	volSize int
	volID   string
)

var runpodCmd = &cobra.Command{
	Use:   "runpod",
	Short: "Manage RunPod resources",
}

var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Manage RunPod network volumes",
}

var volumeCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new network volume",
	RunE: func(cmd *cobra.Command, args []string) error {
		key := apiKey
		if key == "" {
			key = os.Getenv("RUNPOD_API_KEY")
		}
		if key == "" {
			return fmt.Errorf("api key is required (set via -k or RUNPOD_API_KEY)")
		}

		ctx := context.Background()
		id, err := iocore.CreateNetworkVolume(ctx, key, volName, volSize, region)
		if err != nil {
			return err
		}

		fmt.Printf("Successfully created network volume!\nID: %s\n", id)
		return nil
	},
}

var volumeDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a network volume",
	RunE: func(cmd *cobra.Command, args []string) error {
		key := apiKey
		if key == "" {
			key = os.Getenv("RUNPOD_API_KEY")
		}
		if key == "" {
			return fmt.Errorf("api key is required (set via -k or RUNPOD_API_KEY)")
		}

		ctx := context.Background()
		if volID == "" && len(args) > 0 {
			volID = args[0]
		}
		if volID == "" {
			return fmt.Errorf("volume ID is required")
		}

		err := iocore.DeleteNetworkVolume(ctx, key, volID)
		if err != nil {
			return err
		}

		fmt.Printf("Successfully deleted network volume %s\n", volID)
		return nil
	},
}

var volumeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all network volumes",
	RunE: func(cmd *cobra.Command, args []string) error {
		key := apiKey
		if key == "" {
			key = os.Getenv("RUNPOD_API_KEY")
		}
		if key == "" {
			return fmt.Errorf("api key is required (set via -k or RUNPOD_API_KEY)")
		}

		ctx := context.Background()
		volumes, err := iocore.ListNetworkVolumes(ctx, key)
		if err != nil {
			return err
		}

		table := tablewriter.NewTable(os.Stdout,
			tablewriter.WithHeader([]string{"ID", "Name", "Size (GB)", "Data Center", "Status"}),
		)
		for _, v := range volumes {
			table.Append([]string{v.ID, v.Name, fmt.Sprintf("%d", v.Size), v.DataCenterID, v.Status})
		}
		table.Render()
		return nil
	},
}

func init() {
	volumeCreateCmd.Flags().StringVar(&volName, "name", "", "volume name")
	volumeCreateCmd.Flags().IntVar(&volSize, "size", 10, "volume size in GB")
	volumeCreateCmd.Flags().StringVar(&region, "region", "EU-RO-1", "data center ID")

	volumeDeleteCmd.Flags().StringVar(&volID, "id", "", "volume ID")

	volumeCmd.AddCommand(volumeCreateCmd)
	volumeCmd.AddCommand(volumeDeleteCmd)
	volumeCmd.AddCommand(volumeListCmd)
	runpodCmd.AddCommand(volumeCmd)
	rootCmd.AddCommand(runpodCmd)
}
