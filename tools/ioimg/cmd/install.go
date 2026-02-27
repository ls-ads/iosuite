package cmd

import (
	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var installModel string

func init() {
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install required models and binaries",
		RunE: func(cmd *cobra.Command, args []string) error {
			if installModel == "" {
				installModel = "ffmpeg"
			}
			return iocore.InstallModel(cmd.Context(), installModel)
		},
	}

	installCmd.Flags().StringVarP(&installModel, "model", "m", "ffmpeg", "Model to install (e.g. ffmpeg)")
	rootCmd.AddCommand(installCmd)
}
