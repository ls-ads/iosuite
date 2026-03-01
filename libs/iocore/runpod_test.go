package iocore

import (
	"reflect"
	"testing"
)

func TestBuildVolumeJobInput(t *testing.T) {
	tests := []struct {
		name           string
		endpointID     string
		templateID     string
		inputFileName  string
		outputFileName string
		ffmpegArgs     string
		outputExt      string
		expected       map[string]interface{}
	}{
		{
			name:           "Real-ESRGAN img endpoint",
			endpointID:     "iosuite-img-real-esrgan",
			templateID:     "047z8w5i69",
			inputFileName:  "test.jpg",
			outputFileName: "out_test.jpg",
			ffmpegArgs:     "",
			outputExt:      "png",
			expected: map[string]interface{}{
				"image_path":    "/runpod-volume/test.jpg",
				"output_path":   "/runpod-volume/out_test.jpg",
				"output_format": "png",
			},
		},
		{
			name:           "FFmpeg endpoint",
			endpointID:     "iosuite-ffmpeg",
			templateID:     "uduo7jdyhn",
			inputFileName:  "test.mp4",
			outputFileName: "out_test.mp4",
			ffmpegArgs:     "-vf,scale=1280:720",
			outputExt:      "mp4",
			expected: map[string]interface{}{
				"input_path":  "/runpod-volume/test.mp4",
				"output_path": "/runpod-volume/out_test.mp4",
				"ffmpeg_args": "-vf,scale=1280:720",
			},
		},
		{
			name:           "Generic img template",
			endpointID:     "some-endpoint",
			templateID:     "047z8w5i69",
			inputFileName:  "image.png",
			outputFileName: "out_image.png",
			ffmpegArgs:     "",
			outputExt:      "jpg",
			expected: map[string]interface{}{
				"image_path":    "/runpod-volume/image.png",
				"output_path":   "/runpod-volume/out_image.png",
				"output_format": "jpg",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildVolumeJobInput(tt.endpointID, tt.templateID, tt.inputFileName, tt.outputFileName, tt.ffmpegArgs, tt.outputExt)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("buildVolumeJobInput() = %v, want %v", got, tt.expected)
			}
		})
	}
}
