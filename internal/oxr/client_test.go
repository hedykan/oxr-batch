package oxr

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/parse/sync" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(1 << 20)
		if err != nil {
			t.Fatal(err)
		}
		if got := form.Value["api_key"][0]; got != "dummy" {
			t.Fatalf("api_key = %q", got)
		}
		formats := form.Value["output_formats"]
		if len(formats) != 2 || formats[0] != "markdown" || formats[1] != "json" {
			t.Fatalf("formats = %#v", formats)
		}
		file, err := form.File["file"][0].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if string(body) != "image" {
			t.Fatalf("file body = %q", body)
		}
		payload := `{"code":0,"message":"ok","data":{"task_id":"t-1","metadata":{"page_num":1},"result":{"outputs":{"markdown":"hello","json":{"pages":[]}}}}}`
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(payload))}, nil
	})

	path := filepath.Join(t.TempDir(), "page.png")
	if err := os.WriteFile(path, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := New("http://oxr.test/api/v1", "dummy")
	client.HTTPClient.Transport = transport
	result, err := client.Parse(context.Background(), path, ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Markdown != "hello" || result.Pages != 1 || result.TaskID != "t-1" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestParseTooLarge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.pdf")
	_ = os.WriteFile(path, []byte("pdf"), 0o644)
	client := New("http://oxr.test/api/v1", "dummy")
	client.HTTPClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusRequestEntityTooLarge, Body: io.NopCloser(bytes.NewBufferString("too large"))}, nil
	})
	_, err := client.Parse(context.Background(), path, ParseOptions{})
	if err != ErrTooLarge {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
