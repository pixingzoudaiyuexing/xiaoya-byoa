package aliyundrive_share2_open

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/go-resty/resty/v2"
)

func TestAliyunTempCopyCleanupModeOffDoesNotDelete(t *testing.T) {
	deleteIDs := runAliyunTempCopyLink(t, "off")
	if len(deleteIDs) != 0 {
		t.Fatalf("cleanup=off deleted file IDs %v, want none", deleteIDs)
	}
}

func TestAliyunTempCopyCleanupModeImmediateDeletesExactNewFile(t *testing.T) {
	deleteIDs := runAliyunTempCopyLink(t, "immediate")
	if len(deleteIDs) != 1 || deleteIDs[0] != "new-copy-id" {
		t.Fatalf("cleanup=immediate deleted file IDs %v, want [new-copy-id]", deleteIDs)
	}
}

func runAliyunTempCopyLink(t *testing.T, cleanupMode string) []string {
	t.Helper()
	t.Setenv("BYOA_ALIYUN_TEMP_CLEANUP", cleanupMode)
	var deleteIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive":
			_, _ = w.Write([]byte(`{"default_drive_id":"visitor-drive"}`))
		case "/create":
			_, _ = w.Write([]byte(`{"file_id":"byoa-temp-folder"}`))
		case "/copy":
			_, _ = w.Write([]byte(`{"responses":[{"body":{"file_id":"new-copy-id"}}]}`))
		case "/preview":
			_, _ = w.Write([]byte(`{"video_preview_play_info":{"live_transcoding_task_list":[{"template_id":"HD","status":"finished","url":"https://example.com/full.m3u8"}]}}`))
		case "/delete":
			var body struct {
				FileID string `json:"file_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode cleanup request: %v", err)
			}
			deleteIDs = append(deleteIDs, body.FileID)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldDrive, oldCreate := aliyunDriveInfoEndpoint, aliyunFileCreateEndpoint
	oldCopy, oldPreview, oldDelete := aliyunFileCopyEndpoint, aliyunFilePreviewEndpoint, aliyunFileDeleteEndpoint
	aliyunDriveInfoEndpoint, aliyunFileCreateEndpoint = server.URL+"/drive", server.URL+"/create"
	aliyunFileCopyEndpoint, aliyunFilePreviewEndpoint, aliyunFileDeleteEndpoint = server.URL+"/copy", server.URL+"/preview", server.URL+"/delete"
	t.Cleanup(func() {
		aliyunDriveInfoEndpoint, aliyunFileCreateEndpoint = oldDrive, oldCreate
		aliyunFileCopyEndpoint, aliyunFilePreviewEndpoint, aliyunFileDeleteEndpoint = oldCopy, oldPreview, oldDelete
	})

	driver := &AliyundriveShare2Open{Addition: Addition{ShareId: "share-id"}}
	file := &model.Object{ID: "source-file-id", Name: "movie.mkv"}
	link, err := driver.byoaTempCopyLink(context.Background(), file, "visitor-token")
	if err != nil {
		t.Fatalf("byoaTempCopyLink() error = %v", err)
	}
	if link.URL != "https://example.com/full.m3u8" {
		t.Fatalf("link URL = %q", link.URL)
	}
	return deleteIDs
}

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
