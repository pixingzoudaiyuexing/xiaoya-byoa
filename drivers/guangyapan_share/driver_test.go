package guangyapan_share

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/guangyapan"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestNormalizeShareID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "raw share id",
			in:   "1894369771769081942_aeWVzywV3ZOZly47",
			want: "1894369771769081942_aeWVzywV3ZOZly47",
		},
		{
			name: "share url",
			in:   "https://www.guangyapan.com/s/1894369771769081942_aeWVzywV3ZOZly47",
			want: "1894369771769081942_aeWVzywV3ZOZly47",
		},
		{
			name: "share url with fragment",
			in:   "https://www.guangyapan.com/s/1894369771769081942_aeWVzywV3ZOZly47#/",
			want: "1894369771769081942_aeWVzywV3ZOZly47",
		},
		{
			name: "invalid input",
			in:   "https://example.com/not-guangya",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeShareID(tt.in); got != tt.want {
				t.Fatalf("normalizeShareID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGuangYaPanShareLink_CachesByShareIDAndFileID(t *testing.T) {
	origCache := guangYaPanShareLinkCache
	origResolver := resolveGuangYaPanShareLink
	guangYaPanShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveGuangYaPanShareLink = func(ctx context.Context, d *GuangYaPanShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/" + d.ShareID + "/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		guangYaPanShareLinkCache = origCache
		resolveGuangYaPanShareLink = origResolver
	})

	file := &model.Object{ID: "file-1", Name: "video.mkv"}
	firstDriver := &GuangYaPanShare{Addition: Addition{ShareID: "share-a"}}
	secondDriver := &GuangYaPanShare{Addition: Addition{ShareID: "share-b"}}

	first, err := firstDriver.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("first link error: %v", err)
	}
	second, err := firstDriver.Link(context.Background(), file, model.LinkArgs{Type: "ignored"})
	if err != nil {
		t.Fatalf("second link error: %v", err)
	}
	third, err := secondDriver.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("third link error: %v", err)
	}

	if first.URL != second.URL {
		t.Fatalf("expected cached result for identical share/file, got %q and %q", first.URL, second.URL)
	}
	if first.URL == third.URL {
		t.Fatalf("expected different shares to use different cache buckets, got %q", first.URL)
	}
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice, got %d", resolveCalls)
	}
}

func TestGuangYaPanShareLink_DoesNotCacheNilOrError(t *testing.T) {
	origCache := guangYaPanShareLinkCache
	origResolver := resolveGuangYaPanShareLink
	guangYaPanShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveGuangYaPanShareLink = func(ctx context.Context, d *GuangYaPanShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		if resolveCalls == 1 {
			return nil, nil
		}
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		guangYaPanShareLinkCache = origCache
		resolveGuangYaPanShareLink = origResolver
	})

	d := &GuangYaPanShare{Addition: Addition{ShareID: "share-a"}}
	file := &model.Object{ID: "file-1", Name: "video.mkv"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice after nil/error results, got %d", resolveCalls)
	}
}

func TestFindRestoredFile_PrefersExactNameAndSize(t *testing.T) {
	files := []model.Obj{
		&model.Object{ID: "wrong-name", Name: "other.mkv", Size: 100, Modified: time.Unix(100, 0)},
		&model.Object{ID: "wrong-size", Name: "target.mkv", Size: 200, Modified: time.Unix(200, 0)},
		&model.Object{ID: "exact-old", Name: "target.mkv", Size: 100, Modified: time.Unix(300, 0)},
		&model.Object{ID: "exact-new", Name: "target.mkv", Size: 100, Modified: time.Unix(400, 0)},
	}

	file, ok := findRestoredFile(files, &model.Object{Name: "target.mkv", Size: 100})
	if !ok {
		t.Fatal("expected restored file match")
	}
	if file.GetID() != "exact-new" {
		t.Fatalf("expected newest exact match, got %q", file.GetID())
	}
}

func TestFileToObj(t *testing.T) {
	got := fileToObj(shareFile{
		FileID:    "file-1",
		FileName:  "video.mkv",
		FileSize:  123,
		ResType:   1,
		UTime:     1776942528,
		Thumbnail: "https://example.com/thumb.jpg",
	})

	obj, ok := got.(*model.ObjThumb)
	if !ok {
		t.Fatalf("expected *model.ObjThumb, got %T", got)
	}
	if obj.GetID() != "file-1" || obj.GetName() != "video.mkv" || obj.GetSize() != 123 {
		t.Fatalf("unexpected object mapping: %+v", obj)
	}
	if obj.IsDir() {
		t.Fatal("expected file object")
	}
	if obj.Thumbnail.Thumbnail != "https://example.com/thumb.jpg" {
		t.Fatalf("unexpected thumbnail: %q", obj.Thumbnail.Thumbnail)
	}
}

// SaveTo 服务端转存:restore_share 批量 fileIds、parentId 直达目标目录;
// 目录对象回退字节中转;新 id 经转存前后目标目录差集解析。
func TestGuangYaPanShareSaveTo(t *testing.T) {
	var gotIDs []string
	var gotParent string
	calls := 0
	origTask, origList := saveGuangyaTask, listGuangyaTarget
	saveGuangyaTask = func(ctx context.Context, d *GuangYaPanShare, account *guangyapan.GuangYaPan, fileIDs []string, parentID string) (string, error) {
		gotIDs, gotParent = fileIDs, parentID
		return "task-1", nil
	}
	listGuangyaTarget = func(ctx context.Context, account *guangyapan.GuangYaPan, dir model.Obj) (map[string]struct{}, error) {
		calls++
		if calls == 1 {
			return map[string]struct{}{"old": {}}, nil
		}
		return map[string]struct{}{"old": {}, "9001": {}, "9002": {}}, nil
	}
	t.Cleanup(func() { saveGuangyaTask, listGuangyaTarget = origTask, origList })

	d := &GuangYaPanShare{}
	d.ShareAccessToken = "TOK"
	dst := &model.Object{ID: "target-dir", Name: "我的追剧/剧名", IsFolder: true}

	saved, err := d.SaveTo(context.Background(), &guangyapan.GuangYaPan{}, dst, []model.Obj{
		&model.Object{ID: "f1", Name: "第01集.mkv"},
		&model.Object{ID: "f2", Name: "第02集.mkv"},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Join(gotIDs, ",") != "f1,f2" || gotParent != "target-dir" {
		t.Errorf("restore params: got ids=%v parent=%q", gotIDs, gotParent)
	}
	got := map[string]bool{}
	for _, id := range saved {
		got[id] = true
	}
	if !got["9001"] || !got["9002"] || got["old"] {
		t.Errorf("diff must return only new ids, got %v", saved)
	}

	// 目录对象:拒绝并回退字节中转
	_, err = d.SaveTo(context.Background(), &guangyapan.GuangYaPan{}, dst,
		[]model.Obj{&model.Object{ID: "dir1", Name: "剧名", IsFolder: true}})
	if err == nil || !strings.Contains(err.Error(), "目录") {
		t.Fatalf("expected dir rejection, got %v", err)
	}
}

// 转存任务失败错误上抛,由调用方回退字节中转 copy。
func TestGuangYaPanShareSaveTo_TaskErrorPropagates(t *testing.T) {
	origTask, origList := saveGuangyaTask, listGuangyaTarget
	saveGuangyaTask = func(ctx context.Context, d *GuangYaPanShare, account *guangyapan.GuangYaPan, fileIDs []string, parentID string) (string, error) {
		return "", errors.New("share expired")
	}
	listGuangyaTarget = func(ctx context.Context, account *guangyapan.GuangYaPan, dir model.Obj) (map[string]struct{}, error) {
		return map[string]struct{}{}, nil
	}
	t.Cleanup(func() { saveGuangyaTask, listGuangyaTarget = origTask, origList })

	d := &GuangYaPanShare{}
	d.ShareAccessToken = "TOK"
	dst := &model.Object{ID: "1", Name: "剧名", IsFolder: true}
	_, err := d.SaveTo(context.Background(), &guangyapan.GuangYaPan{}, dst,
		[]model.Obj{&model.Object{ID: "f1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "share expired") {
		t.Fatalf("expected task error to propagate, got %v", err)
	}
}

// 目标存储不是光鸭账号驱动时拒绝,不发起任何请求。
func TestGuangYaPanShareSaveTo_RejectsNonGuangyaTarget(t *testing.T) {
	called := false
	origTask, origList := saveGuangyaTask, listGuangyaTarget
	saveGuangyaTask = func(ctx context.Context, d *GuangYaPanShare, account *guangyapan.GuangYaPan, fileIDs []string, parentID string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { saveGuangyaTask, listGuangyaTarget = origTask, origList })

	d := &GuangYaPanShare{}
	dst := &model.Object{ID: "1", Name: "剧名", IsFolder: true}
	_, err := d.SaveTo(context.Background(), nil, dst,
		[]model.Obj{&model.Object{ID: "f1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "光鸭云盘账号") {
		t.Fatalf("expected non-guangya target rejection, got %v", err)
	}
	if called {
		t.Fatal("rejection must not trigger any restore request")
	}
}
