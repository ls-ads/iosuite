package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var (
	upscaleProvider string
	apiKey          string
	model           string
	dryRun          bool
)

type batchMetrics struct {
	TotalFiles      int
	Success         int
	Failure         int
	TotalTime       time.Duration
	TotalBilledTime time.Duration
	TotalCost       float64
	InputBytes      int64
	OutputBytes     int64
	IsDryRun        bool
	Files           []fileMetric
}

type fileMetric struct {
	Name     string
	Duration time.Duration
	Cost     float64
	Success  bool
	Error    string
}

var upscaleCmd = &cobra.Command{
	Use:   "upscale",
	Short: "Upscale images using local or remote providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		if input == "" || output == "" {
			return fmt.Errorf("input and output are required")
		}

		config := iocore.UpscaleConfig{
			Provider: iocore.UpscaleProvider(upscaleProvider),
			APIKey:   apiKey,
			Model:    model,
		}

		return processPath(input, output, &config)
	},
}

var upscaleInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize and provision required infrastructure for the upscaler",
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := iocore.UpscaleProvider(upscaleProvider)

		if provider != iocore.ProviderRunPod {
			fmt.Printf("Initialization is not required for provider: %s\n", provider)
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
		fmt.Printf("Provisioning RunPod endpoint '%s'...\n", iocore.RunPodIOImgEndpointName)
		fmt.Println("This may take 10+ minutes depending on template size and GPU availability.")

		endpointID, err := iocore.EnsureRunPodEndpoint(ctx, key, iocore.RunPodEndpointConfig{
			Name:        iocore.RunPodIOImgEndpointName,
			TemplateID:  "047z8w5i69",
			GPUTypeIDs:  []string{"NVIDIA RTX A4000"}, // 16GB tier
			WorkersMin:  0,
			WorkersMax:  1,
			IdleTimeout: 5,
			Flashboot:   true,
		})

		if err != nil {
			return fmt.Errorf("failed to provision runpod endpoint: %v", err)
		}

		fmt.Printf("Successfully initialized RunPod endpoint!\nEndpoint ID: %s\n", endpointID)
		return nil
	},
}

type upscaleJob struct {
	src string
	dst string
}

func processPath(src, dst string, config *iocore.UpscaleConfig) error {
	var jobs []upscaleJob

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !isImage(path) {
				return nil
			}

			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			jobs = append(jobs, upscaleJob{
				src: path,
				dst: filepath.Join(dst, rel),
			})
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		if !isImage(src) {
			return fmt.Errorf("input file is not a supported image: %s", src)
		}
		target := dst
		dstInfo, err := os.Stat(dst)
		if err == nil && dstInfo.IsDir() {
			target = filepath.Join(dst, filepath.Base(src))
		}
		jobs = append(jobs, upscaleJob{src: src, dst: target})
	}

	if len(jobs) == 0 {
		return fmt.Errorf("no images found to process")
	}

	metrics := &batchMetrics{
		TotalFiles: len(jobs),
		IsDryRun:   dryRun,
	}
	startAll := time.Now()

	useProgressBar := config.Provider != iocore.ProviderRunPod

	var bar *progressbar.ProgressBar
	if useProgressBar {
		bar = progressbar.NewOptions(len(jobs),
			progressbar.OptionSetDescription("Upscaling"),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSpinnerType(14),
			progressbar.OptionFullWidth(),
			progressbar.OptionClearOnFinish(),
		)
	}

	// Wire up StatusCallback for RunPod progress updates BEFORE creating upscaler
	if config.Provider == iocore.ProviderRunPod {
		config.StatusCallback = func(update iocore.RunPodStatusUpdate) {
			elapsed := ""
			if update.Elapsed > 0 {
				elapsed = fmt.Sprintf(" (%s)", update.Elapsed.Round(time.Second))
			}
			msg := fmt.Sprintf("[RunPod] %s%s", update.Message, elapsed)
			// Overwrite the current line on stderr
			fmt.Fprintf(os.Stderr, "\r%-60s", msg)
			if update.Phase == "completed" {
				// Clear the status line
				fmt.Fprintf(os.Stderr, "\r%-60s\r", "")
			}
		}
	}

	// Create upscaler AFTER callback is set so it's captured in the config copy
	upscaler, err := iocore.NewUpscaler(*config)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		currentFile := filepath.Base(job.src)
		avgTime := time.Duration(0)
		if len(metrics.Files) > 0 {
			avgTime = time.Since(startAll) / time.Duration(len(metrics.Files))
		}

		if useProgressBar {
			descPrefix := "Upscaling"
			if dryRun {
				descPrefix = "[DRY-RUN] Estimating"
			}
			bar.Describe(fmt.Sprintf("%s [S:%d|F:%d|Avg:%s|Cost:$%.4f] %s",
				descPrefix, metrics.Success, metrics.Failure, avgTime.Round(time.Millisecond), metrics.TotalCost, currentFile))
		}

		var inSize, outSize int64
		var activeDuration, wallDuration time.Duration
		var err error

		if dryRun {
			info, _ := os.Stat(job.src)
			inSize = info.Size()
			activeDuration = estimateDuration(upscaleProvider, inSize)
			wallDuration = 100 * time.Millisecond // Dry run is fast
			outSize = inSize * 16                 // Rough estimate (4x4 = 16)
		} else {
			start := time.Now()
			inSize, outSize, activeDuration, err = upscaleFile(job.src, job.dst, upscaler)
			wallDuration = time.Since(start)
		}

		cost := calculateCost(upscaleProvider, activeDuration)

		metric := fileMetric{
			Name:     currentFile,
			Duration: wallDuration,
			Cost:     cost,
			Success:  err == nil,
		}

		if err != nil {
			metric.Error = err.Error()
			metrics.Failure++
		} else {
			metrics.Success++
			metrics.InputBytes += inSize
			metrics.OutputBytes += outSize
			metrics.TotalCost += cost
			metrics.TotalBilledTime += activeDuration
		}
		metrics.Files = append(metrics.Files, metric)
		if useProgressBar {
			bar.Add(1)
		}
		if dryRun {
			time.Sleep(50 * time.Millisecond) // Just to make the bar visible
		}
	}
	metrics.TotalTime = time.Since(startAll)

	displayMetrics(metrics)
	return nil
}

func estimateDuration(provider string, size int64) time.Duration {
	mb := float64(size) / (1024 * 1024)
	if provider == "local" {
		return time.Second + time.Duration(mb*5)*time.Second
	}
	// Remote
	return 5*time.Second + time.Duration(mb*10)*time.Second
}

func calculateCost(provider string, duration time.Duration) float64 {
	seconds := duration.Seconds()
	switch provider {
	case "replicate":
		// ~$0.000225 per second (T4 GPU)
		return seconds * 0.000225
	case "runpod":
		// ~$0.00019 per second (Standard mid-range), rounded up to the next full second as per RunPod policy
		billingSeconds := float64(int(seconds))
		if seconds > billingSeconds {
			billingSeconds += 1
		}
		return billingSeconds * 0.00019
	case "local":
		fallthrough
	default:
		return 0.0
	}
}

func upscaleFile(src, dst string, upscaler iocore.Upscaler) (int64, int64, time.Duration, error) {
	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return 0, 0, 0, err
	}

	inFile, err := os.Open(src)
	if err != nil {
		return 0, 0, 0, err
	}
	defer inFile.Close()

	inInfo, _ := inFile.Stat()
	inSize := inInfo.Size()

	outFile, err := os.Create(dst)
	if err != nil {
		return inSize, 0, 0, err
	}
	defer outFile.Close()

	activeDuration, err := upscaler.Upscale(context.Background(), inFile, outFile)
	outSize := int64(0)
	if err == nil {
		outInfo, _ := outFile.Stat()
		outSize = outInfo.Size()
	}

	return inSize, outSize, activeDuration, err
}

func displayMetrics(m *batchMetrics) {
	title := "Upscale Summary"
	if m.IsDryRun {
		title = "Upscale Summary (ESTIMATED - DRY RUN)"
	}
	fmt.Printf("\n\n--- %s ---\n", title)
	table := tablewriter.NewTable(os.Stdout,
		tablewriter.WithHeader([]string{"Metric", "Value"}),
	)

	avgTime := time.Duration(0)
	avgCost := 0.0
	if m.Success > 0 {
		avgTime = m.TotalTime / time.Duration(m.TotalFiles)
		avgCost = m.TotalCost / float64(m.TotalFiles)
	}

	data := [][]string{
		{"Total Files", fmt.Sprintf("%d", m.TotalFiles)},
		{"Succeeded", fmt.Sprintf("%d", m.Success)},
		{"Failed", fmt.Sprintf("%d", m.Failure)},
		{"Total Time", m.TotalTime.Round(time.Millisecond).String()},
		{"Billed Time", m.TotalBilledTime.Round(time.Millisecond).String()},
		{"Avg Time/Img", avgTime.Round(time.Millisecond).String()},
		{"Total Cost", fmt.Sprintf("$%.4f", m.TotalCost)},
		{"Avg Cost/Img", fmt.Sprintf("$%.4f", avgCost)},
		{"Input Size", formatBytes(m.InputBytes)},
		{"Output Size", formatBytes(m.OutputBytes)},
	}
	for _, row := range data {
		table.Append(row[0], row[1])
	}
	table.Render()

	if m.Failure > 0 {
		fmt.Println("\nErrors:")
		for _, f := range m.Files {
			if !f.Success {
				fmt.Printf("  - %s: %s\n", f.Name, f.Error)
			}
		}
	}
	fmt.Println()
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func isImage(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp":
		return true
	}
	return false
}

func init() {
	upscaleCmd.Flags().StringVarP(&upscaleProvider, "provider", "p", "local", "Upscale provider (local, replicate, runpod)")
	upscaleCmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API Key for remote provider")
	upscaleCmd.Flags().StringVarP(&model, "model", "m", "real-esrgan", "Model, Version, or Endpoint ID")
	upscaleCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Estimate time/cost without actually upscaling")

	upscaleInitCmd.Flags().StringVarP(&upscaleProvider, "provider", "p", "local", "Upscale provider (local, replicate, runpod)")
	upscaleInitCmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API Key for remote provider")

	upscaleCmd.AddCommand(upscaleInitCmd)
	rootCmd.AddCommand(upscaleCmd)
}
