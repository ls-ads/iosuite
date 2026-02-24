package cmd

import (
	"context"
	"encoding/json"
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
	jsonOutput      bool
	outputFormat    string
	recursive       bool
	overwrite       bool
)

type batchMetrics struct {
	TotalFiles      int
	Skipped         int
	Success         int
	Failure         int
	TotalTime       time.Duration
	TotalBilledTime time.Duration
	TotalCost       float64
	InputBytes      int64
	OutputBytes     int64
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
	Use:          "upscale",
	Short:        "Upscale images using local or remote providers",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if input == "" {
			return fmt.Errorf("input is required")
		}

		// Derive output if not specified
		if output == "" {
			info, err := os.Stat(input)
			if err != nil {
				return err
			}
			if info.IsDir() {
				output = strings.TrimRight(input, string(filepath.Separator)) + "_out"
			} else {
				ext := filepath.Ext(input)
				output = strings.TrimSuffix(input, ext) + "_out" + ext
			}
		}

		// Validate --format if explicitly set
		if outputFormat != "" && outputFormat != "jpg" && outputFormat != "png" {
			return fmt.Errorf("unsupported output format: %s (must be jpg or png)", outputFormat)
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
		endpointName := iocore.GetRunPodEndpointName(model)
		fmt.Printf("Provisioning RunPod endpoint '%s'...\n", endpointName)
		fmt.Println("This may take 10+ minutes depending on template size and GPU availability.")

		endpointID, err := iocore.EnsureRunPodEndpoint(ctx, key, iocore.RunPodEndpointConfig{
			Name:        endpointName,
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

var upscaleModelCmd = &cobra.Command{
	Use:   "model",
	Short: "Manage upscale models",
}

var upscaleModelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available upscale models",
	Run: func(cmd *cobra.Command, args []string) {
		table := tablewriter.NewTable(os.Stdout,
			tablewriter.WithHeader([]string{"Model", "Scale", "Providers"}),
		)
		table.Append("real-esrgan", "4x", "local, replicate, runpod")
		table.Render()
	},
}

var upscaleProviderCmd = &cobra.Command{
	Use:   "provider",
	Short: "Manage upscale providers",
}

var upscaleProviderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available upscale providers",
	Run: func(cmd *cobra.Command, args []string) {
		table := tablewriter.NewTable(os.Stdout,
			tablewriter.WithHeader([]string{"Provider", "Type", "Requires API Key"}),
		)
		table.Append("local", "Local GPU (ncnn-vulkan)", "No")
		table.Append("replicate", "Cloud API", "Yes (REPLICATE_API_KEY)")
		table.Append("runpod", "Cloud API", "Yes (RUNPOD_API_KEY)")
		table.Render()
	},
}

type upscaleJob struct {
	src    string
	dst    string
	format string
}

func processPath(src, dst string, config *iocore.UpscaleConfig) error {
	var jobs []upscaleJob

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	isBatch := info.IsDir()

	if isBatch {
		if recursive {
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
				outPath := filepath.Join(dst, rel)

				// Resolve format and rename extension
				fmt_, err := resolveOutputFormat(path, outputFormat)
				if err != nil {
					return nil // skip unsupported format silently
				}
				outPath = changeExt(outPath, "."+fmt_)

				jobs = append(jobs, upscaleJob{src: path, dst: outPath, format: fmt_})
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			entries, err := os.ReadDir(src)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				path := filepath.Join(src, entry.Name())
				if !isImage(path) {
					continue
				}

				outPath := filepath.Join(dst, entry.Name())

				fmt_, err := resolveOutputFormat(path, outputFormat)
				if err != nil {
					continue // skip unsupported format silently
				}
				outPath = changeExt(outPath, "."+fmt_)

				jobs = append(jobs, upscaleJob{src: path, dst: outPath, format: fmt_})
			}
		}
	} else {
		if !isImage(src) {
			return fmt.Errorf("input file is not a supported image: %s", src)
		}

		fmt_, err := resolveOutputFormat(src, outputFormat)
		if err != nil {
			return err
		}

		target := dst
		dstInfo, err := os.Stat(dst)
		if err == nil && dstInfo.IsDir() {
			target = filepath.Join(dst, filepath.Base(src))
		}
		target = changeExt(target, "."+fmt_)

		jobs = append(jobs, upscaleJob{src: src, dst: target, format: fmt_})
	}

	if len(jobs) == 0 {
		return fmt.Errorf("no images found to process")
	}

	// Skip files whose output already exists (unless --overwrite is set)
	totalFound := len(jobs)
	if !overwrite {
		var filtered []upscaleJob
		for _, job := range jobs {
			if _, err := os.Stat(job.dst); err == nil {
				continue
			}
			filtered = append(filtered, job)
		}
		if skipped := totalFound - len(filtered); skipped > 0 && len(filtered) == 0 {
			fmt.Fprintf(os.Stderr, "All %d images already processed. Use --overwrite to reprocess.\n", skipped)
			return nil
		}
		jobs = filtered
	}

	metrics := &batchMetrics{
		TotalFiles: totalFound,
		Skipped:    totalFound - len(jobs),
	}
	startAll := time.Now()

	// Wire up StatusCallback for RunPod progress updates BEFORE creating upscaler
	batchStarted := false
	if config.Provider == iocore.ProviderRunPod {
		config.StatusCallback = func(update iocore.RunPodStatusUpdate) {
			if batchStarted {
				return // progress bar handles display during batch processing
			}
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
	upscaler, err := iocore.NewUpscaler(context.Background(), config)
	if config.Provider == iocore.ProviderRunPod {
		fmt.Fprintf(os.Stderr, "\r%-60s\r", "") // clear status line
	}
	if err != nil {
		return err
	}

	var bar *progressbar.ProgressBar
	if isBatch {
		batchStarted = true
		bar = progressbar.NewOptions(len(jobs),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionSetWidth(30),
			progressbar.OptionShowCount(),
			progressbar.OptionClearOnFinish(),
		)
		bar.RenderBlank()
	}

	for _, job := range jobs {
		var inSize, outSize int64
		var activeDuration, wallDuration time.Duration
		var err error

		start := time.Now()
		config.OutputFormat = job.format
		inSize, outSize, activeDuration, err = upscaleFile(job.src, job.dst, upscaler)
		wallDuration = time.Since(start)

		cost := calculateCost(upscaleProvider, activeDuration)

		metric := fileMetric{
			Name:     filepath.Base(job.src),
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

		if bar != nil {
			bar.Add(1)
		}
	}
	metrics.TotalTime = time.Since(startAll)

	if bar != nil {
		bar.Clear()
	}

	displayMetrics(metrics)
	return nil
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
	if jsonOutput {
		avgTime := time.Duration(0)
		avgCost := 0.0
		if m.Success > 0 {
			avgTime = m.TotalBilledTime / time.Duration(m.Success)
			avgCost = m.TotalCost / float64(m.Success)
		}

		files := []map[string]interface{}{}
		for _, f := range m.Files {
			entry := map[string]interface{}{
				"name":     f.Name,
				"duration": f.Duration.Round(time.Millisecond).String(),
				"cost":     f.Cost,
				"success":  f.Success,
			}
			if !f.Success {
				entry["error"] = f.Error
			}
			files = append(files, entry)
		}

		result := map[string]interface{}{
			"total_files":  m.TotalFiles,
			"skipped":      m.Skipped,
			"succeeded":    m.Success,
			"failed":       m.Failure,
			"total_time":   m.TotalTime.Round(time.Millisecond).String(),
			"billed_time":  m.TotalBilledTime.Round(time.Millisecond).String(),
			"avg_time_img": avgTime.Round(time.Millisecond).String(),
			"total_cost":   m.TotalCost,
			"avg_cost_img": avgCost,
			"input_bytes":  m.InputBytes,
			"output_bytes": m.OutputBytes,
			"files":        files,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
		return
	}

	table := tablewriter.NewTable(os.Stdout,
		tablewriter.WithHeader([]string{"Metric", "Value"}),
	)

	avgTime := time.Duration(0)
	avgCost := 0.0
	if m.Success > 0 {
		avgTime = m.TotalBilledTime / time.Duration(m.Success)
		avgCost = m.TotalCost / float64(m.Success)
	}

	data := [][]string{
		{"Total Files", fmt.Sprintf("%d", m.TotalFiles)},
		{"Skipped", fmt.Sprintf("%d", m.Skipped)},
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
	case ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

// resolveOutputFormat determines the output format for a given input file.
// If flagFormat is set, it takes precedence. Otherwise, the format is derived
// from the input file extension.
func resolveOutputFormat(inputPath, flagFormat string) (string, error) {
	if flagFormat != "" {
		return flagFormat, nil
	}
	ext := strings.ToLower(filepath.Ext(inputPath))
	switch ext {
	case ".jpg", ".jpeg":
		return "jpg", nil
	case ".png":
		return "png", nil
	default:
		return "", fmt.Errorf("unsupported image format: %s", ext)
	}
}

// changeExt replaces the file extension of path with newExt (e.g. ".png").
func changeExt(path, newExt string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + newExt
}

func init() {
	upscaleCmd.Flags().StringVarP(&upscaleProvider, "provider", "p", "local", "Upscale provider")
	upscaleCmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API key for remote provider")
	upscaleCmd.Flags().StringVarP(&model, "model", "m", "real-esrgan", "Upscale model")
	upscaleCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")
	upscaleCmd.Flags().StringVarP(&outputFormat, "format", "f", "", "Output format: jpg or png (default: match input)")
	upscaleCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Recursively process subdirectories")
	upscaleCmd.Flags().BoolVar(&overwrite, "overwrite", false, "Reprocess all files even if output already exists")

	upscaleInitCmd.Flags().StringVarP(&upscaleProvider, "provider", "p", "local", "Upscale provider")
	upscaleInitCmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API key for remote provider")
	upscaleInitCmd.Flags().StringVarP(&model, "model", "m", "real-esrgan", "Upscale model")

	upscaleModelCmd.AddCommand(upscaleModelListCmd)
	upscaleProviderCmd.AddCommand(upscaleProviderListCmd)
	upscaleCmd.AddCommand(upscaleInitCmd)
	upscaleCmd.AddCommand(upscaleModelCmd)
	upscaleCmd.AddCommand(upscaleProviderCmd)
	rootCmd.AddCommand(upscaleCmd)
}
