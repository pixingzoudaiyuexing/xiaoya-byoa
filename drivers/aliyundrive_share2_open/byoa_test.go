package aliyundrive_share2_open

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/go-resty/resty/v2"
)

func TestAliyunBYOARequiresBrowserCredential(t *testing.T) {
	d := &AliyundriveShare2Open{}

	_, err := d.Link(context.Background(), nil, model.LinkArgs{})
	if err == nil {
		t.Fatal("Link() expected BYOA auth error")
	}
	if !byoa.IsAuthRequired(err) {
		t.Fatalf("Link() error = %T %v, want AuthRequiredError", err, err)
	}

	var authErr *byoa.AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("errors.As() failed for %T", err)
	}
	if authErr.Provider != byoa.ProviderAliyun {
		t.Fatalf("provider = %q, want %q", authErr.Provider, byoa.ProviderAliyun)
	}
}

func TestAliyunBYOAFallsBackToShareVideoPreview(t *testing.T) {
	downloadCalls := 0
	previewCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download":
			downloadCalls++
			w.WriteHeader(http.StatusGone)
		case "/preview":
			previewCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer\ttest-access-token" {
				t.Fatalf("Authorization = %q", got)
			}
			if got := r.Header.Get("x-share-token"); got != "test-share-token" {
				t.Fatalf("x-share-token = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			for key, want := range map[string]string{
				"drive_id": "test-drive-id",
				"file_id":  "test-file-id",
				"share_id": "test-share-id",
				"category": "live_transcoding",
			} {
				if got := body[key]; got != want {
					t.Fatalf("%s = %v, want %q", key, got, want)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"video_preview_play_info":{"live_transcoding_task_list":[{"template_id":"HD","status":"finished","preview_url":"https://example.com/preview.m3u8"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldDownloadEndpoint := aliyunBYOAShareDownloadEndpoint
	oldPreviewEndpoint := aliyunBYOASharePreviewEndpoint
	aliyunBYOAShareDownloadEndpoint = server.URL + "/download"
	aliyunBYOASharePreviewEndpoint = server.URL + "/preview"
	t.Cleanup(func() {
		aliyunBYOAShareDownloadEndpoint = oldDownloadEndpoint
		aliyunBYOASharePreviewEndpoint = oldPreviewEndpoint
	})

	url, apiErr, err := requestAliyunBYOAShareURL(
		resty.New(),
		context.Background(),
		"test-access-token",
		"test-share-token",
		"test-drive-id",
		"test-file-id",
		"test-share-id",
	)
	if err != nil {
		t.Fatalf("requestAliyunBYOAShareURL() error = %v", err)
	}
	if apiErr != nil && apiErr.Code != "" {
		t.Fatalf("requestAliyunBYOAShareURL() api error = %q", apiErr.Code)
	}
	if got, want := url, "https://example.com/preview.m3u8"; got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
	if downloadCalls != 1 || previewCalls != 1 {
		t.Fatalf("calls: download=%d preview=%d, want 1 each", downloadCalls, previewCalls)
	}
}
