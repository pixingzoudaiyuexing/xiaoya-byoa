package aliyundrive_share2_open

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/go-resty/resty/v2"
)

type tempCopyScenario struct {
	cleanupMode           *string
	previewStatus         int
	deleteStatus          int
	cancelParentAfterCopy bool
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

func TestAliyunTempCopyCleanupSurvivesCanceledParent(t *testing.T) {
	result := runAliyunTempCopyLink(t, tempCopyScenario{cancelParentAfterCopy: true})
	if result.err == nil {
		t.Fatal("expected playback to fail after parent cancellation")
	}
	assertExactCleanupID(t, result.deleteIDs)
}

func TestAliyunTempCopyFailureInvalidatesOnlyCurrentDriveFolder(t *testing.T) {
	driveID := "stale-drive-" + t.Name()
	otherDriveID := "other-drive-" + t.Name()
	aliyunTempFolderCache.Store(driveID, "stale-folder")
	aliyunTempFolderCache.Store(otherDriveID, "other-folder")
	t.Cleanup(func() {
		aliyunTempFolderCache.Delete(driveID)
		aliyunTempFolderCache.Delete(otherDriveID)
	})

	var copyCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive":
			_, _ = fmt.Fprintf(w, `{"default_drive_id":%q}`, driveID)
		case "/copy":
			copyCalls.Add(1)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"code":"CopyFailed","message":"private detail"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	restore := setAliyunTempCopyEndpoints(t, server.URL)
	defer restore()

	driver := &AliyundriveShare2Open{Addition: Addition{ShareId: "share", ShareToken: "ready"}}
	_, err := driver.byoaTempCopyLink(context.Background(), &model.Object{ID: "source"}, "token")
	if err == nil || err.Error() != "aliyun temporary operation http status: 502" {
		t.Fatalf("copy error = %v, want original sanitized error", err)
	}
	if copyCalls.Load() != 1 {
		t.Fatalf("copy calls = %d, want 1 without same-request retry", copyCalls.Load())
	}
	if _, ok := aliyunTempFolderCache.Load(driveID); ok {
		t.Fatal("stale current-drive folder cache was not invalidated")
	}
	if got, ok := aliyunTempFolderCache.Load(otherDriveID); !ok || got != "other-folder" {
		t.Fatalf("unrelated cache entry = %v, %t", got, ok)
	}
}

func TestAliyunTempCopyNextRequestSelfHealsStaleFolder(t *testing.T) {
	off := "off"
	t.Setenv("BYOA_ALIYUN_TEMP_CLEANUP", off)
	driveID := "self-heal-drive-" + t.Name()
	aliyunTempFolderCache.Store(driveID, "stale-folder")
	t.Cleanup(func() { aliyunTempFolderCache.Delete(driveID) })

	var copyCalls atomic.Int32
	var listCalls atomic.Int32
	var createCalls atomic.Int32
	var copyParentIDs []string
	var copyMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive":
			_, _ = fmt.Fprintf(w, `{"default_drive_id":%q}`, driveID)
		case "/list":
			listCalls.Add(1)
			_, _ = w.Write([]byte(`{"items":[],"next_marker":""}`))
		case "/create":
			createCalls.Add(1)
			_, _ = w.Write([]byte(`{"file_id":"new-folder"}`))
		case "/copy":
			var body struct {
				Requests []struct {
					Body struct {
						ParentID string `json:"to_parent_file_id"`
					} `json:"body"`
				} `json:"requests"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode copy request: %v", err)
			}
			parentID := body.Requests[0].Body.ParentID
			copyMu.Lock()
			copyParentIDs = append(copyParentIDs, parentID)
			copyMu.Unlock()
			if copyCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(`{"responses":[{"body":{"file_id":"new-copy-id"}}]}`))
		case "/preview":
			_, _ = w.Write([]byte(`{"video_preview_play_info":{"live_transcoding_task_list":[{"template_id":"HD","status":"finished","url":"https://example.com/full.m3u8"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	restore := setAliyunTempCopyEndpoints(t, server.URL)
	defer restore()

	driver := &AliyundriveShare2Open{Addition: Addition{ShareId: "share", ShareToken: "ready"}}
	file := &model.Object{ID: "source"}
	if _, err := driver.byoaTempCopyLink(context.Background(), file, "token"); err == nil {
		t.Fatal("first request expected copy failure")
	}
	if copyCalls.Load() != 1 {
		t.Fatalf("first request copy calls = %d, want 1", copyCalls.Load())
	}
	link, err := driver.byoaTempCopyLink(context.Background(), file, "token")
	if err != nil || link == nil || link.URL != "https://example.com/full.m3u8" {
		t.Fatalf("second request link=%v err=%v", link, err)
	}
	if listCalls.Load() != 1 || createCalls.Load() != 1 || copyCalls.Load() != 2 {
		t.Fatalf("list=%d create=%d copy=%d", listCalls.Load(), createCalls.Load(), copyCalls.Load())
	}
	if len(copyParentIDs) != 2 || copyParentIDs[0] != "stale-folder" || copyParentIDs[1] != "new-folder" {
		t.Fatalf("copy parent IDs = %v", copyParentIDs)
	}
	if got, ok := aliyunTempFolderCache.Load(driveID); !ok || got != "new-folder" {
		t.Fatalf("healed cache = %v, %t", got, ok)
	}
}

func setAliyunTempCopyEndpoints(t *testing.T, baseURL string) func() {
	t.Helper()
	oldDrive, oldList, oldCreate := aliyunDriveInfoEndpoint, aliyunFileListEndpoint, aliyunFileCreateEndpoint
	oldCopy, oldPreview, oldDelete := aliyunFileCopyEndpoint, aliyunFilePreviewEndpoint, aliyunFileDeleteEndpoint
	aliyunDriveInfoEndpoint, aliyunFileListEndpoint, aliyunFileCreateEndpoint = baseURL+"/drive", baseURL+"/list", baseURL+"/create"
	aliyunFileCopyEndpoint, aliyunFilePreviewEndpoint, aliyunFileDeleteEndpoint = baseURL+"/copy", baseURL+"/preview", baseURL+"/delete"
	return func() {
		aliyunDriveInfoEndpoint, aliyunFileListEndpoint, aliyunFileCreateEndpoint = oldDrive, oldList, oldCreate
		aliyunFileCopyEndpoint, aliyunFilePreviewEndpoint, aliyunFileDeleteEndpoint = oldCopy, oldPreview, oldDelete
	}
}

func TestAliyunBYOAAccessTokenErrorsRequireReauth(t *testing.T) {
	for _, code := range []string{"AccessTokenInvalid", "AccessTokenExpired"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = fmt.Fprintf(w, `{"code":%q,"message":"secret upstream detail"}`, code)
			}))
			defer server.Close()
			oldDrive := aliyunDriveInfoEndpoint
			aliyunDriveInfoEndpoint = server.URL
			t.Cleanup(func() { aliyunDriveInfoEndpoint = oldDrive })

			driver := &AliyundriveShare2Open{Addition: Addition{ShareId: "share", ShareToken: "ready"}}
			_, err := driver.byoaDirectLink(context.Background(), &model.Object{ID: "source"}, "visitor-token")
			var authErr *byoa.AuthRequiredError
			if !errors.As(err, &authErr) || authErr.Provider != byoa.ProviderAliyun {
				t.Fatalf("error = %T %v, want Aliyun AuthRequiredError", err, err)
			}
			if strings.Contains(err.Error(), "secret upstream detail") {
				t.Fatalf("upstream message leaked: %v", err)
			}
		})
	}
}

func TestAliyunBYOAGenericErrorIsSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"code":"InternalError","message":"private upstream body"}`))
	}))
	defer server.Close()
	err := aliyunBYOARequest(context.Background(), resty.New(), "token", "", server.URL, map[string]string{}, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "http status: 502") {
		t.Fatalf("error = %v, want sanitized status", err)
	}
	if strings.Contains(err.Error(), "private upstream body") || strings.Contains(err.Error(), "InternalError") {
		t.Fatalf("upstream response leaked: %v", err)
	}
}

func TestAliyunTempFolderCachesResolvedFolder(t *testing.T) {
	var listCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"file_id":"cached-folder","name":".Xiaoya-BYOA-Temp","type":"folder"}],"next_marker":""}`))
	}))
	defer server.Close()
	oldList, oldCreate := aliyunFileListEndpoint, aliyunFileCreateEndpoint
	aliyunFileListEndpoint, aliyunFileCreateEndpoint = server.URL, server.URL
	t.Cleanup(func() { aliyunFileListEndpoint, aliyunFileCreateEndpoint = oldList, oldCreate })

	driveID := "cache-" + t.Name()
	first, err := aliyunBYOATempFolder(context.Background(), resty.New(), "token", driveID)
	if err != nil || first != "cached-folder" {
		t.Fatalf("first resolve = %q, %v", first, err)
	}
	before := listCalls.Load()
	second, err := aliyunBYOATempFolder(context.Background(), resty.New(), "different-token", driveID)
	if err != nil || second != first || listCalls.Load() != before {
		t.Fatalf("cache hit = %q, %v, calls %d -> %d", second, err, before, listCalls.Load())
	}
}

func TestAliyunTempFolderFindsFolderAfterFirstPage(t *testing.T) {
	var listCalls atomic.Int32
	firstPage := make([]map[string]string, 100)
	for i := range firstPage {
		firstPage[i] = map[string]string{"file_id": fmt.Sprintf("other-%d", i), "name": fmt.Sprintf("other-%d", i), "type": "folder"}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["marker"] == "page-2" {
			listCalls.Add(1)
			_, _ = w.Write([]byte(`{"items":[{"file_id":"second-page-folder","name":".Xiaoya-BYOA-Temp","type":"folder"}],"next_marker":""}`))
			return
		}
		listCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": firstPage, "next_marker": "page-2"})
	}))
	defer server.Close()
	oldList, oldCreate := aliyunFileListEndpoint, aliyunFileCreateEndpoint
	aliyunFileListEndpoint, aliyunFileCreateEndpoint = server.URL, server.URL+"/unexpected-create"
	t.Cleanup(func() { aliyunFileListEndpoint, aliyunFileCreateEndpoint = oldList, oldCreate })

	folder, err := aliyunBYOATempFolder(context.Background(), resty.New(), "token", "paged-"+t.Name())
	if err != nil || folder != "second-page-folder" || listCalls.Load() != 2 {
		t.Fatalf("folder=%q err=%v listCalls=%d", folder, err, listCalls.Load())
	}
}

func TestAliyunTempFolderCreatesAndCachesMissingFolder(t *testing.T) {
	var listCalls atomic.Int32
	var createCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/list":
			listCalls.Add(1)
			_, _ = w.Write([]byte(`{"items":[],"next_marker":""}`))
		case "/create":
			createCalls.Add(1)
			_, _ = w.Write([]byte(`{"file_id":"created-folder"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	oldList, oldCreate := aliyunFileListEndpoint, aliyunFileCreateEndpoint
	aliyunFileListEndpoint, aliyunFileCreateEndpoint = server.URL+"/list", server.URL+"/create"
	t.Cleanup(func() { aliyunFileListEndpoint, aliyunFileCreateEndpoint = oldList, oldCreate })

	driveID := "create-" + t.Name()
	for i := 0; i < 2; i++ {
		folder, err := aliyunBYOATempFolder(context.Background(), resty.New(), "token", driveID)
		if err != nil || folder != "created-folder" {
			t.Fatalf("resolve %d = %q, %v", i, folder, err)
		}
	}
	if listCalls.Load() != 1 || createCalls.Load() != 1 {
		t.Fatalf("list calls=%d create calls=%d, want 1 each", listCalls.Load(), createCalls.Load())
	}
}

func TestAliyunTempFolderSerializesConcurrentCreation(t *testing.T) {
	var listCalls atomic.Int32
	var createCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/list":
			listCalls.Add(1)
			_, _ = w.Write([]byte(`{"items":[],"next_marker":""}`))
		case "/create":
			createCalls.Add(1)
			_, _ = w.Write([]byte(`{"file_id":"single-folder"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	oldList, oldCreate := aliyunFileListEndpoint, aliyunFileCreateEndpoint
	aliyunFileListEndpoint, aliyunFileCreateEndpoint = server.URL+"/list", server.URL+"/create"
	t.Cleanup(func() { aliyunFileListEndpoint, aliyunFileCreateEndpoint = oldList, oldCreate })

	driveID := "concurrent-" + t.Name()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			folder, err := aliyunBYOATempFolder(context.Background(), resty.New(), "token", driveID)
			if err != nil || folder != "single-folder" {
				errs <- fmt.Errorf("folder=%q err=%v", folder, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if listCalls.Load() != 1 || createCalls.Load() != 1 {
		t.Fatalf("list calls=%d create calls=%d, want 1 each", listCalls.Load(), createCalls.Load())
	}
}

func TestAliyunTempCopyClientHasBoundedTimeout(t *testing.T) {
	client := newAliyunBYOAClient()
	if got := client.GetClient().Timeout; got <= 0 || got > 15*time.Second {
		t.Fatalf("HTTP timeout = %v, want >0 and <=15s", got)
	}
}

func TestAliyunCleanupContextHasFiniteDeadline(t *testing.T) {
	ctx, cancel := newAliyunBYOACleanupContext()
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("cleanup context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > 10*time.Second {
		t.Fatalf("cleanup deadline remaining = %v", remaining)
	}
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive":
			_, _ = fmt.Fprintf(w, `{"default_drive_id":%q}`, "visitor-"+t.Name())
		case "/list":
			_, _ = w.Write([]byte(`{"items":[],"next_marker":""}`))
		case "/create":
			_, _ = w.Write([]byte(`{"file_id":"byoa-temp-folder"}`))
		case "/copy":
			_, _ = w.Write([]byte(`{"responses":[{"body":{"file_id":"new-copy-id"}}]}`))
		case "/preview":
			if scenario.cancelParentAfterCopy {
				cancel()
			}
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

	oldDrive, oldList, oldCreate := aliyunDriveInfoEndpoint, aliyunFileListEndpoint, aliyunFileCreateEndpoint
	oldCopy, oldPreview, oldDelete := aliyunFileCopyEndpoint, aliyunFilePreviewEndpoint, aliyunFileDeleteEndpoint
	aliyunDriveInfoEndpoint, aliyunFileListEndpoint, aliyunFileCreateEndpoint = server.URL+"/drive", server.URL+"/list", server.URL+"/create"
	aliyunFileCopyEndpoint, aliyunFilePreviewEndpoint, aliyunFileDeleteEndpoint = server.URL+"/copy", server.URL+"/preview", server.URL+"/delete"
	t.Cleanup(func() {
		aliyunDriveInfoEndpoint, aliyunFileListEndpoint, aliyunFileCreateEndpoint = oldDrive, oldList, oldCreate
		aliyunFileCopyEndpoint, aliyunFilePreviewEndpoint, aliyunFileDeleteEndpoint = oldCopy, oldPreview, oldDelete
	})

	driver := &AliyundriveShare2Open{Addition: Addition{ShareId: "share-id"}}
	file := &model.Object{ID: "source-file-id", Name: "movie.mkv"}
	link, err := driver.byoaTempCopyLink(ctx, file, "visitor-token")
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
