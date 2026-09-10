package aliyundrive_share2_open

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
)

func TestAliyunTempCopyCleanupRequiresExactCreatedFile(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()
	old := aliyunFileDeleteEndpoint
	aliyunFileDeleteEndpoint = server.URL
	t.Cleanup(func() { aliyunFileDeleteEndpoint = old })

	err := aliyunBYOACleanup(context.Background(), resty.New(), "token", aliyunTempCopy{driveID: "drive", parentID: "folder", fileID: ""})
	if err == nil || called {
		t.Fatal("empty copied file_id must refuse cleanup without a request")
	}
	err = aliyunBYOACleanup(context.Background(), resty.New(), "token", aliyunTempCopy{driveID: "drive", parentID: "root", fileID: "existing"})
	if err == nil || called {
		t.Fatal("root/non-owned destination must refuse cleanup without a request")
	}
}

func TestAliyunBYOAVideoURLRequiresFinishedAndPrefersFullURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"video_preview_play_info":{"live_transcoding_task_list":[{"template_id":"FHD","status":"running","url":"https://bad"},{"template_id":"HD","status":"finished","url":"https://full","preview_url":"https://preview"}]}}`))
	}))
	defer server.Close()
	old := aliyunFilePreviewEndpoint
	aliyunFilePreviewEndpoint = server.URL
	t.Cleanup(func() { aliyunFilePreviewEndpoint = old })

	url, err := aliyunBYOAVideoURL(context.Background(), resty.New(), "token", aliyunTempCopy{driveID: "drive", parentID: "folder", fileID: "new"})
	if err != nil || url != "https://full" {
		t.Fatalf("url=%q err=%v", url, err)
	}
}

func TestAliyunBYOARequestSanitizesTooManyRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rule;TooManyRequests secret", http.StatusTooManyRequests)
	}))
	defer server.Close()
	err := aliyunBYOARequest(context.Background(), resty.New(), "token", "", server.URL, map[string]string{}, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "阿里云请求过于频繁") || strings.Contains(err.Error(), "TooManyRequests") {
		t.Fatalf("err=%v", err)
	}
}
