package _189_share

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	_189pc "github.com/OpenListTeam/OpenList/v4/drivers/189pc"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	gocache "github.com/OpenListTeam/go-cache"
)

func TestCloud189ShareLink_CachesByFileID(t *testing.T) {
	origCache := cloud189ShareLinkCache
	origResolver := resolveCloud189ShareLink
	cloud189ShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveCloud189ShareLink = func(ctx context.Context, d *Cloud189Share, file model.Obj) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/189/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		cloud189ShareLinkCache = origCache
		resolveCloud189ShareLink = origResolver
	})

	d := &Cloud189Share{}
	file := &FileObj{ObjThumb: model.ObjThumb{Object: model.Object{ID: "file-1", Name: "video.mp4"}}}

	first, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("first link: %v", err)
	}
	second, err := d.Link(context.Background(), file, model.LinkArgs{Type: "ignored"})
	if err != nil {
		t.Fatalf("second link: %v", err)
	}
	if first.URL != second.URL {
		t.Fatalf("expected cached URL reuse, got %q and %q", first.URL, second.URL)
	}
	if resolveCalls != 1 {
		t.Fatalf("expected resolver once, got %d", resolveCalls)
	}
}

func TestCloud189ShareLink_UsesDifferentKeysForDifferentFiles(t *testing.T) {
	origCache := cloud189ShareLinkCache
	origResolver := resolveCloud189ShareLink
	cloud189ShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveCloud189ShareLink = func(ctx context.Context, d *Cloud189Share, file model.Obj) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/189/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		cloud189ShareLinkCache = origCache
		resolveCloud189ShareLink = origResolver
	})

	d := &Cloud189Share{}
	file1 := &FileObj{ObjThumb: model.ObjThumb{Object: model.Object{ID: "file-1", Name: "a.mp4"}}}
	file2 := &FileObj{ObjThumb: model.ObjThumb{Object: model.Object{ID: "file-2", Name: "b.mp4"}}}

	_, _ = d.Link(context.Background(), file1, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file2, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice for different file IDs, got %d", resolveCalls)
	}
}

func TestCloud189ShareLink_DoesNotCacheErrors(t *testing.T) {
	origCache := cloud189ShareLinkCache
	origResolver := resolveCloud189ShareLink
	cloud189ShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveCloud189ShareLink = func(ctx context.Context, d *Cloud189Share, file model.Obj) (*model.Link, error) {
		resolveCalls++
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		cloud189ShareLinkCache = origCache
		resolveCloud189ShareLink = origResolver
	})

	d := &Cloud189Share{}
	file := &FileObj{ObjThumb: model.ObjThumb{Object: model.Object{ID: "file-1", Name: "video.mp4"}}}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice after errors, got %d", resolveCalls)
	}
}

// SaveTo 服务端转存:SHARE_SAVE 批量任务带 shareId+目标目录,taskInfos 按 obj 拼装
// (FileId/FileName/IsFolder);新 id 优先取任务状态 successedFileIdList。
func TestCloud189ShareSaveTo(t *testing.T) {
	d := &Cloud189Share{Addition: Addition{ShareId: "SHARECODE1"}}
	shareTokenCache.Set(d.ShareId, ShareInfo{ShareId: 777, FileId: "root", IsFolder: true},
		gocache.WithEx[ShareInfo](time.Minute))

	var gotShareId int
	var gotTarget string
	var gotInfos []_189pc.BatchTaskInfo
	origTask, origList := shareSaveTask, list189Target
	shareSaveTask = func(ctx context.Context, pc *_189pc.Cloud189PC, shareId int, targetFolderId string, infos []_189pc.BatchTaskInfo) ([]string, error) {
		gotShareId, gotTarget, gotInfos = shareId, targetFolderId, infos
		return []string{"9001", "9002"}, nil
	}
	list189Target = func(ctx context.Context, pc *_189pc.Cloud189PC, dir model.Obj) (map[string]struct{}, error) {
		return map[string]struct{}{"a": {}}, nil
	}
	t.Cleanup(func() {
		shareSaveTask, list189Target = origTask, origList
	})

	objs := []model.Obj{
		&model.Object{ID: "f1", Name: "第01集.mkv"},
		&model.Object{ID: "dir1", Name: "剧名", IsFolder: true},
	}
	dst := &model.Object{ID: "target-dir", Name: "剧", IsFolder: true}
	saved, err := d.SaveTo(context.Background(), &_189pc.Cloud189PC{}, dst, objs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotShareId != 777 || gotTarget != "target-dir" {
		t.Errorf("task params: got shareId=%v target=%q want 777/target-dir", gotShareId, gotTarget)
	}
	if len(gotInfos) != 2 || gotInfos[0].FileId != "f1" || gotInfos[0].FileName != "第01集.mkv" || gotInfos[0].IsFolder != 0 {
		t.Errorf("file taskInfo mismatch: %+v", gotInfos[0])
	}
	if gotInfos[1].FileId != "dir1" || gotInfos[1].IsFolder != 1 {
		t.Errorf("dir taskInfo must carry IsFolder=1: %+v", gotInfos[1])
	}
	if strings.Join(saved, ",") != "9001,9002" {
		t.Errorf("saved ids from task state: got %v want [9001 9002]", saved)
	}
}

// 任务状态不回报 id(目录转存形态)→ 回退转存前后目标目录差集解析。
func TestCloud189ShareSaveTo_FallbackToDirDiff(t *testing.T) {
	d := &Cloud189Share{Addition: Addition{ShareId: "SHARECODE2"}}
	shareTokenCache.Set(d.ShareId, ShareInfo{ShareId: 778, FileId: "root"},
		gocache.WithEx[ShareInfo](time.Minute))

	calls := 0
	origTask, origList := shareSaveTask, list189Target
	shareSaveTask = func(ctx context.Context, pc *_189pc.Cloud189PC, shareId int, targetFolderId string, infos []_189pc.BatchTaskInfo) ([]string, error) {
		return []string{}, nil // 任务状态无 id
	}
	list189Target = func(ctx context.Context, pc *_189pc.Cloud189PC, dir model.Obj) (map[string]struct{}, error) {
		calls++
		if calls == 1 {
			return map[string]struct{}{"old": {}}, nil
		}
		return map[string]struct{}{"old": {}, "9001": {}, "9002": {}}, nil
	}
	t.Cleanup(func() {
		shareSaveTask, list189Target = origTask, origList
	})

	dst := &model.Object{ID: "target-dir", Name: "剧", IsFolder: true}
	saved, err := d.SaveTo(context.Background(), &_189pc.Cloud189PC{}, dst,
		[]model.Obj{&model.Object{ID: "dir1", Name: "剧名", IsFolder: true}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := map[string]bool{}
	for _, id := range saved {
		got[id] = true
	}
	if !got["9001"] || !got["9002"] {
		t.Errorf("diff fallback must return new ids, got %v", saved)
	}
}

// SHARE_SAVE 任务失败(非冲突)错误上抛,由调用方回退字节中转 copy。
func TestCloud189ShareSaveTo_TaskErrorPropagates(t *testing.T) {
	d := &Cloud189Share{Addition: Addition{ShareId: "SHARECODE3"}}
	shareTokenCache.Set(d.ShareId, ShareInfo{ShareId: 779, FileId: "root"},
		gocache.WithEx[ShareInfo](time.Minute))

	origTask, origList := shareSaveTask, list189Target
	shareSaveTask = func(ctx context.Context, pc *_189pc.Cloud189PC, shareId int, targetFolderId string, infos []_189pc.BatchTaskInfo) ([]string, error) {
		return nil, errors.New("转存目录不存在")
	}
	list189Target = func(ctx context.Context, pc *_189pc.Cloud189PC, dir model.Obj) (map[string]struct{}, error) {
		return nil, nil
	}
	t.Cleanup(func() {
		shareSaveTask, list189Target = origTask, origList
	})

	dst := &model.Object{ID: "target-dir", Name: "剧", IsFolder: true}
	_, err := d.SaveTo(context.Background(), &_189pc.Cloud189PC{}, dst,
		[]model.Obj{&model.Object{ID: "f1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "转存目录不存在") {
		t.Fatalf("expected task error to propagate, got %v", err)
	}
}

// 目标存储不是天翼账号驱动时拒绝,不发起任何请求。
func TestCloud189ShareSaveTo_RejectsNon189Target(t *testing.T) {
	called := false
	origTask, origList := shareSaveTask, list189Target
	shareSaveTask = func(ctx context.Context, pc *_189pc.Cloud189PC, shareId int, targetFolderId string, infos []_189pc.BatchTaskInfo) ([]string, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { shareSaveTask, list189Target = origTask, origList })

	d := &Cloud189Share{}
	dst := &model.Object{ID: "1", Name: "剧", IsFolder: true}
	_, err := d.SaveTo(context.Background(), nil, dst,
		[]model.Obj{&model.Object{ID: "f1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "天翼云盘账号") {
		t.Fatalf("expected non-189 target rejection, got %v", err)
	}
	if called {
		t.Fatal("rejection must not trigger any task")
	}
}
