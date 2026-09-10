package aliyundrive_share2_open

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/go-resty/resty/v2"
)

type tempCopyScenario struct {
	cleanupMode   *string
	previewStatus int
	deleteStatus  int
}

type tempCopyResult struct {
	link      *model.Link
	err       error
	deleteIDs []string
}

func TestAliyunTempCopyCleanupDefaultsToImmediate(t *testing.T) {
	result := runAliyunTempCopyLink(t, tempCopyScenario{})
	if result.err != nil || result.link == nil {
		t.Fatalf("default cleanup link=%v err=%v", result.link, result.err)
	}
	assertExactCleanupID(t, result.deleteIDs)
}

func TestAliyunTempCopyCleanupModeOffDoesNotDelete(t *testing.T) {
	off := "off"
	result := runAliyunTempCopyLink(t, tempCopyScenario{cleanupMode: &off})
	if result.err != nil || result.link == nil {
		t.Fatalf("cleanup=off link=%v err=%v", result.link, result.err)
	}
	if len(result.deleteIDs) != 0 {
		t.Fatalf("cleanup=off deleted file IDs %v, want none", result.deleteIDs)
	}
}

func TestAliyunTempCopyUsesBrowserTokenEndpoints(t *testing.T) {
	for name, endpoint := range map[string]string{
		"drive info":    aliyunDriveInfoEndpoint,
		"folder list":   aliyunFileListEndpoint,
		"folder create": aliyunFileCreateEndpoint,
		"video preview": aliyunFilePreviewEndpoint,
		"cleanup":       aliyunFileDeleteEndpoint,
	} {
		if !strings.HasPrefix(endpoint, "https://api.alipan.com/") {
			t.Errorf("%s endpoint = %q, want api.alipan.com", name, endpoint)
		}
	}
	if !strings.HasPrefix(aliyunFileCopyEndpoint, "https://api.alipan.com/") {
		t.Fatalf("copy endpoint = %q, want api.alipan.com", aliyunFileCopyEndpoint)
	}
}

func TestAliyunTempCopyCleanupModeImmediateDeletesExactNewFile(t *testing.T) {
	immediate := "immediate"
	result := runAliyunTempCopyLink(t, tempCopyScenario{cleanupMode: &immediate})
	if result.err != nil || result.link == nil {
		t.Fatalf("cleanup=immediate link=%v err=%v", result.link, result.err)
	}
	assertExactCleanupID(t, result.deleteIDs)
}

func TestAliyunTempCopyReturnsPlaybackWhenCleanupFails(t *testing.T) {
	result := runAliyunTempCopyLink(t, tempCopyScenario{deleteStatus: http.StatusInternalServerError})
	if result.err != nil || result.link == nil || result.link.URL != "https://example.com/full.m3u8" {
		t.Fatalf("cleanup failure suppressed playback: link=%v err=%v", result.link, result.err)
	}
	assertExactCleanupID(t, result.deleteIDs)
}

func TestAliyunTempCopyCleansUpWhenPlaybackFails(t *testing.T) {
	result := runAliyunTempCopyLink(t, tempCopyScenario{previewStatus: http.StatusBadGateway})
	if result.err == nil || !strings.Contains(result.err.Error(), "http status: 502") {
		t.Fatalf("playback error = %v, want sanitized 502 error", result.err)
	}
	assertExactCleanupID(t, result.deleteIDs)
}

func TestAliyunTempCopyPreservesPlaybackErrorWhenCleanupFails(t *testing.T) {
	result := runAliyunTempCopyLink(t, tempCopyScenario{
		previewStatus: http.StatusBadGateway,
		deleteStatus:  http.StatusInternalServerError,
	})
	if result.err == nil || result.err.Error() != "aliyun temporary operation http status: 502" {
		t.Fatalf("error = %v, want original playback error", result.err)
	}
	assertExactCleanupID(t, result.deleteIDs)
}

func assertExactCleanupID(t *testing.T, deleteIDs []string) {
	t.Helper()
	if len(deleteIDs) != 1 || deleteIDs[0] != "new-copy-id" {
		t.Fatalf("cleanup deleted file IDs %v, want [new-copy-id]", deleteIDs)
	}
}

func runAliyunTempCopyLink(t *testing.T, scenario tempCopyScenario) tempCopyResult {
	t.Helper()
	oldMode, hadMode := os.LookupEnv("BYOA_ALIYUN_TEMP_CLEANUP")
	if scenario.cleanupMode == nil {
		if err := os.Unsetenv("BYOA_ALIYUN_TEMP_CLEANUP"); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Setenv("BYOA_ALIYUN_TEMP_CLEANUP", *scenario.cleanupMode)
	}
	t.Cleanup(func() {
		if hadMode {
			_ = os.Setenv("BYOA_ALIYUN_TEMP_CLEANUP", oldMode)
		} else {
			_ = os.Unsetenv("BYOA_ALIYUN_TEMP_CLEANUP")
		}
	})
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
			if scenario.previewStatus != 0 {
				w.WriteHeader(scenario.previewStatus)
				return
			}
			_, _ = w.Write([]byte(`{"video_preview_play_info":{"live_transcoding_task_list":[{"template_id":"HD","status":"finished","url":"https://example.com/full.m3u8"}]}}`))
		case "/delete":
			var body struct {
				FileID string `json:"file_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode cleanup request: %v", err)
			}
			deleteIDs = append(deleteIDs, body.FileID)
			if scenario.deleteStatus != 0 {
				w.WriteHeader(scenario.deleteStatus)
				return
			}
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
	return tempCopyResult{link: link, err: err, deleteIDs: deleteIDs}
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
