package serve

import (
	"bytes"
	"errors"
	"mime/multipart"
	"strings"
	"testing"
)

// extractImageField is what the multipart→JSON translation hangs on,
// so it gets the most coverage. The runpod provider is otherwise
// network-dependent; we trust httptest in a future round to cover
// that surface.

func TestExtractImageField_FindsImagePart(t *testing.T) {
	body, ct := buildMultipart(map[string]string{"image": "the-image-bytes"})
	got, err := extractImageField(body, ct)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "the-image-bytes" {
		t.Errorf("got %q, want %q", string(got), "the-image-bytes")
	}
}

func TestExtractImageField_ErrorsWhenImagePartMissing(t *testing.T) {
	body, ct := buildMultipart(map[string]string{"other": "data"})
	_, err := extractImageField(body, ct)
	if err == nil {
		t.Fatal("expected error when no image part, got nil")
	}
	if !strings.Contains(err.Error(), "image") {
		t.Errorf("error should mention the missing field name: %v", err)
	}
}

func TestExtractImageField_RejectsNonMultipartContentType(t *testing.T) {
	_, err := extractImageField(strings.NewReader("nope"), "application/json")
	if err == nil {
		t.Fatal("expected error for non-multipart content-type")
	}
}

func TestExtractImageField_RejectsContentTypeWithoutBoundary(t *testing.T) {
	_, err := extractImageField(strings.NewReader("nope"), "multipart/form-data")
	if err == nil {
		t.Fatal("expected error when boundary param is absent")
	}
	if !strings.Contains(err.Error(), "boundary") {
		t.Errorf("error should name the missing boundary parameter: %v", err)
	}
}

func TestDecodeRunPodResponse_HappyPath(t *testing.T) {
	resp := map[string]any{
		"output": map[string]any{
			"outputs": []any{
				map[string]any{"image_base64": "Zm9v"}, // "foo"
			},
		},
	}
	out, ct, err := decodeRunPodResponse(resp, "jpg")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "foo" {
		t.Errorf("decoded bytes = %q, want %q", string(out), "foo")
	}
	if ct != "image/jpeg" {
		t.Errorf("content-type = %q, want %q", ct, "image/jpeg")
	}
}

func TestDecodeRunPodResponse_PNGContentType(t *testing.T) {
	resp := map[string]any{
		"output": map[string]any{
			"outputs": []any{map[string]any{"image_base64": "Zm9v"}},
		},
	}
	_, ct, err := decodeRunPodResponse(resp, "png")
	if err != nil {
		t.Fatal(err)
	}
	if ct != "image/png" {
		t.Errorf("content-type = %q, want %q", ct, "image/png")
	}
}

func TestDecodeRunPodResponse_PerItemErrorSurfaces(t *testing.T) {
	resp := map[string]any{
		"output": map[string]any{
			"outputs": []any{},
			"_diagnostics": map[string]any{
				"per_item_errors": []any{
					map[string]any{"error": "input WxH exceeds max 1280x1280"},
				},
			},
		},
	}
	_, _, err := decodeRunPodResponse(resp, "jpg")
	if err == nil {
		t.Fatal("expected error when per_item_errors is non-empty")
	}
	var perr *ProviderError
	if !errors.As(err, &perr) {
		t.Errorf("expected ProviderError so HTTP layer maps to 502; got %T", err)
	}
	if !strings.Contains(err.Error(), "exceeds max") {
		t.Errorf("error should include the worker's diagnostic: %v", err)
	}
}

func TestDecodeRunPodResponse_MissingOutputField(t *testing.T) {
	_, _, err := decodeRunPodResponse(map[string]any{"status": "OK"}, "jpg")
	if err == nil {
		t.Fatal("expected error when `output` is missing")
	}
}

// buildMultipart returns a multipart body + Content-Type header for
// the given fields. Keeps the table-driven tests above readable.
func buildMultipart(fields map[string]string) (*bytes.Buffer, string) {
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	for k, v := range fields {
		w, _ := mw.CreateFormFile(k, k+".bin")
		w.Write([]byte(v))
	}
	mw.Close()
	return buf, mw.FormDataContentType()
}
