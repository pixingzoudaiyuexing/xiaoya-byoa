package thunder_share

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/drivers/thunder_browser"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestThunderShareLink_CachesByFileID(t *testing.T) {
	origCache := thunderShareLinkCache
	origResolver := resolveThunderShareLink
	thunderShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveThunderShareLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/thunder/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		thunderShareLinkCache = origCache
		resolveThunderShareLink = origResolver
	})

	d := &ThunderShare{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 1 {
		t.Fatalf("expected resolver once, got %d", resolveCalls)
	}
}

func TestThunderShareLink_DifferentFileIDsDoNotShareCache(t *testing.T) {
	origCache := thunderShareLinkCache
	origResolver := resolveThunderShareLink
	thunderShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveThunderShareLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/thunder/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		thunderShareLinkCache = origCache
		resolveThunderShareLink = origResolver
	})

	d := &ThunderShare{}
	_, _ = d.Link(context.Background(), &model.Object{ID: "file-1", Name: "a.mp4"}, model.LinkArgs{})
	_, _ = d.Link(context.Background(), &model.Object{ID: "file-2", Name: "b.mp4"}, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice for different file IDs, got %d", resolveCalls)
	}
}

func TestThunderShareLink_DoesNotCacheErrors(t *testing.T) {
	origCache := thunderShareLinkCache
	origResolver := resolveThunderShareLink
	thunderShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveThunderShareLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		thunderShareLinkCache = origCache
		resolveThunderShareLink = origResolver
	})

	d := &ThunderShare{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice after errors, got %d", resolveCalls)
	}
}

func TestThunderShareLink_CASBypassesOrdinaryLinkCache(t *testing.T) {
	origCache := thunderShareLinkCache
	origCASResolver := resolveThunderShareCASLink
	origResolver := resolveThunderShareLink
	thunderShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	thunderShareLinkCache.Set("file-1", &model.Link{URL: "https://example.com/stale.cas"})
	casCalls := 0
	resolveThunderShareCASLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		casCalls++
		return &model.Link{URL: fmt.Sprintf("https://example.com/restored/%d", casCalls)}, nil
	}
	ordinaryCalls := 0
	resolveThunderShareLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		ordinaryCalls++
		return nil, errors.New("ordinary resolver must not run")
	}
	t.Cleanup(func() {
		thunderShareLinkCache = origCache
		resolveThunderShareCASLink = origCASResolver
		resolveThunderShareLink = origResolver
	})

	d := &ThunderShare{}
	file := &model.Object{ID: "file-1", Name: "movie.CAS"}
	first, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("first CAS link: %v", err)
	}
	second, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("second CAS link: %v", err)
	}
	if first.URL != "https://example.com/restored/1" || second.URL != "https://example.com/restored/2" {
		t.Fatalf("expected uncached links, got %q and %q", first.URL, second.URL)
	}
	if casCalls != 2 || ordinaryCalls != 0 {
		t.Fatalf("unexpected calls cas=%d ordinary=%d", casCalls, ordinaryCalls)
	}
}

// SaveTo 服务端转存:share/restore 表单 parent_id 直达目标目录、file_ids 批量携带全部对象,
// 新 id 取响应 trace_file_ids 的源 id→新 id 映射。
func TestThunderShareSaveTo(t *testing.T) {
	var gotData base.Json
	orig := thunderShareRestore
	thunderShareRestore = func(ctx context.Context, tb *thunder_browser.ThunderBrowser, data base.Json) (thunderShareRestoreResponse, error) {
		gotData = data
		return thunderShareRestoreResponse{Params: thunderShareRestoreParams{
			TraceFileIDs: `{"src1":"new1","src2":"new2"}`,
		}}, nil
	}
	t.Cleanup(func() { thunderShareRestore = orig })

	d := &ThunderShare{Addition: Addition{ShareId: "SH1"}}
	d.ShareToken = "TOK123"
	dst := &model.Object{ID: "target-dir", Name: "剧", IsFolder: true}
	objs := []model.Obj{
		&model.Object{ID: "src1", Name: "第01集.mkv"},
		&model.Object{ID: "src2", Name: "第02集.mkv"},
		&model.Object{ID: "dir1", Name: "剧名", IsFolder: true},
	}

	saved, err := d.SaveTo(context.Background(), &thunder_browser.ThunderBrowser{}, dst, objs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ids, _ := gotData["file_ids"].([]string); len(ids) != 3 || ids[0] != "src1" || ids[2] != "dir1" {
		t.Errorf("file_ids: got %v want [src1 src2 dir1]", gotData["file_ids"])
	}
	if gotData["parent_id"] != "target-dir" || gotData["share_id"] != "SH1" ||
		gotData["pass_code_token"] != "TOK123" || gotData["specify_parent_id"] != true {
		t.Errorf("restore params mismatch: %v", gotData)
	}
	if strings.Join(saved, ",") != "new1,new2" {
		t.Errorf("saved ids from trace_file_ids: got %v want [new1 new2]", saved)
	}
}

// restore 接口报错时上抛,由调用方回退字节中转 copy。
func TestThunderShareSaveTo_ErrorPropagates(t *testing.T) {
	orig := thunderShareRestore
	thunderShareRestore = func(ctx context.Context, tb *thunder_browser.ThunderBrowser, data base.Json) (thunderShareRestoreResponse, error) {
		return thunderShareRestoreResponse{}, errors.New("no permission for share")
	}
	t.Cleanup(func() { thunderShareRestore = orig })

	d := &ThunderShare{Addition: Addition{ShareId: "SH1"}}
	d.ShareToken = "TOK123"
	dst := &model.Object{ID: "target-dir", Name: "剧", IsFolder: true}
	_, err := d.SaveTo(context.Background(), &thunder_browser.ThunderBrowser{}, dst,
		[]model.Obj{&model.Object{ID: "src1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "no permission for share") {
		t.Fatalf("expected restore error to propagate, got %v", err)
	}
}

// 目标存储不是迅雷账号驱动时拒绝,不发起任何请求。
func TestThunderShareSaveTo_RejectsNonThunderTarget(t *testing.T) {
	called := false
	orig := thunderShareRestore
	thunderShareRestore = func(ctx context.Context, tb *thunder_browser.ThunderBrowser, data base.Json) (thunderShareRestoreResponse, error) {
		called = true
		return thunderShareRestoreResponse{}, nil
	}
	t.Cleanup(func() { thunderShareRestore = orig })

	d := &ThunderShare{}
	dst := &model.Object{ID: "1", Name: "剧", IsFolder: true}
	_, err := d.SaveTo(context.Background(), nil, dst,
		[]model.Obj{&model.Object{ID: "src1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "迅雷云盘账号") {
		t.Fatalf("expected non-thunder target rejection, got %v", err)
	}
	if called {
		t.Fatal("rejection must not trigger any restore request")
	}
}
