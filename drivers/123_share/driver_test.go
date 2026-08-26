package _123Share

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_123 "github.com/OpenListTeam/OpenList/v4/drivers/123"
	_123_open "github.com/OpenListTeam/OpenList/v4/drivers/123_open"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// File 实现了 model.Obj,可直接作为 Link 入参。

// stubThumbDirect 让「无限直链」这一步失败,以便单测覆盖后面的回退分支。
func stubThumbDirect(t *testing.T) {
	orig := resolveThumbDirect
	resolveThumbDirect = func(d *Pan123Share, ctx context.Context, f File, ip string) (*model.Link, error) {
		return nil, errNoThumbLink
	}
	t.Cleanup(func() { resolveThumbDirect = orig })
}

// freshThumbUrl 造一条 t= 还没过期的缩略图形态签名直链。
func freshThumbUrl(expire time.Time) string {
	return fmt.Sprintf("https://user-app-pay-download-cdn.123295.com/123-159/5ea1c7ee/1814511190-0/"+
		"5ea1c7eeba7579136193ec5a2978549f/c-m9013_24_24?v=5&t=%d&r=81M0WB&bzc=1&bzs=303a303a303a30"+
		"&s=%da044f89bc26f32e992ab431e16efb846&bi=3322231004&filename=x.mkv&cache_type=1"+
		"&w=24&h=24&trade_key=123pan-thumbnail&type=video", expire.Unix(), expire.Unix())
}

// TestThumbDirectLink_UsesFreshListUrl 列表里的签名还够久时,直接剥离返回,不再请求任何接口。
func TestThumbDirectLink_UsesFreshListUrl(t *testing.T) {
	expire := time.Now().Add(7 * 24 * time.Hour)
	d := &Pan123Share{}
	f := File{FileName: "video.mkv", FileId: 1, Size: 19069605034, DownloadUrl: freshThumbUrl(expire)}

	link, err := d.thumbDirectLink(context.Background(), f, "")
	if err != nil {
		t.Fatalf("thumbDirectLink: %v", err)
	}
	if want := "/c-m9013?"; !strings.Contains(link.URL, want) {
		t.Fatalf("expected %q in %s", want, link.URL)
	}
	if strings.Contains(link.URL, "trade_key=123pan-thumbnail") {
		t.Fatalf("trade_key not stripped: %s", link.URL)
	}
	if link.Expiration == nil || *link.Expiration <= 0 {
		t.Fatalf("expected positive expiration, got %v", link.Expiration)
	}
	if link.Header.Get("Referer") != "https://user-app-pay-download-cdn.123295.com/" {
		t.Fatalf("unexpected referer: %q", link.Header.Get("Referer"))
	}
}

func TestThumbDirectLink_DirIsSkipped(t *testing.T) {
	d := &Pan123Share{}
	if _, err := d.thumbDirectLink(context.Background(), File{Type: 1}, ""); !errors.Is(err, errNoThumbLink) {
		t.Fatalf("expected errNoThumbLink for dir, got %v", err)
	}
}

// TestPan123ShareLink_UnlimitedFirst 无限直链可用时优先返回,不碰 share/download/info。
func TestPan123ShareLink_UnlimitedFirst(t *testing.T) {
	origThumb := resolveThumbDirect
	thumbCalls := 0
	resolveThumbDirect = func(d *Pan123Share, ctx context.Context, f File, ip string) (*model.Link, error) {
		thumbCalls++
		return &model.Link{URL: "https://example.com/unlimited"}, nil
	}
	t.Cleanup(func() { resolveThumbDirect = origThumb })

	origAnon := resolveAnonLink
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		t.Fatal("anon download/info should not be reached")
		return nil, nil
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	d := &Pan123Share{}
	link, err := d.Link(context.Background(), File{FileName: "video.mp4"}, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL != "https://example.com/unlimited" {
		t.Fatalf("expected unlimited link, got %+v", link)
	}
	if thumbCalls != 1 {
		t.Fatalf("expected thumb resolver once, got %d", thumbCalls)
	}
}

// TestPan123ShareLink_UnlimitedDisabled 配置关掉后跳过无限直链,直接走 download/info。
func TestPan123ShareLink_UnlimitedDisabled(t *testing.T) {
	origThumb := resolveThumbDirect
	resolveThumbDirect = func(d *Pan123Share, ctx context.Context, f File, ip string) (*model.Link, error) {
		t.Fatal("thumb resolver should be skipped when disabled")
		return nil, nil
	}
	t.Cleanup(func() { resolveThumbDirect = origThumb })

	origAnon := resolveAnonLink
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		return &model.Link{URL: "https://example.com/anon-direct"}, nil
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	d := &Pan123Share{}
	d.DisableUnlimited = true
	link, err := d.Link(context.Background(), File{FileName: "video.mp4"}, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL != "https://example.com/anon-direct" {
		t.Fatalf("expected anon link, got %+v", link)
	}
}

func TestPan123ShareLink_AnonymousFirstReturnsAnonLink(t *testing.T) {
	// 无限直链不可用时:匿名 download/info 成功即返回,不走账号路径。
	stubThumbDirect(t)
	origAnon := resolveAnonLink
	anonCalls := 0
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		anonCalls++
		return &model.Link{URL: "https://example.com/anon-direct"}, nil
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	d := &Pan123Share{}
	file := File{FileName: "video.mp4"}

	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL != "https://example.com/anon-direct" {
		t.Fatalf("expected anon direct link, got %+v", link)
	}
	if anonCalls != 1 {
		t.Fatalf("expected anon resolver once, got %d", anonCalls)
	}
}

func TestPan123ShareLink_TrafficLimitShortCircuits(t *testing.T) {
	// 5112 流量包不足:秒传兜底也未命中时,不回退账号,直接透传真实错误。
	stubThumbDirect(t)
	origAnon := resolveAnonLink
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		return nil, err123TrafficLimit
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	origRapid := rapidShareTo123
	rapidShareTo123 = func(f File) *model.Link { return nil }
	t.Cleanup(func() { rapidShareTo123 = origRapid })

	d := &Pan123Share{}
	file := File{FileName: "video.mp4"}

	_, err := d.Link(context.Background(), file, model.LinkArgs{})
	if !errors.Is(err, err123TrafficLimit) {
		t.Fatalf("expected err123TrafficLimit, got %v", err)
	}
}

func TestPan123ShareList_AnonymousFirstReturnsAnonList(t *testing.T) {
	// 无需 123Pan 账号:匿名列目录成功即返回,不走鉴权路径(getFilesAuth)。
	orig := resolveAnonList
	anonCalls := 0
	resolveAnonList = func(d *Pan123Share, ctx context.Context, parentId string) ([]File, error) {
		anonCalls++
		return []File{{FileName: "anon.mp4", FileId: 7}}, nil
	}
	t.Cleanup(func() { resolveAnonList = orig })

	d := &Pan123Share{}
	files, err := d.List(context.Background(), File{}, model.ListArgs{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 1 || files[0].GetName() != "anon.mp4" {
		t.Fatalf("expected anon list, got %+v", files)
	}
	if anonCalls != 1 {
		t.Fatalf("expected anon resolver once, got %d", anonCalls)
	}
}

func TestPan123ShareLink_TrafficLimitRapidFallback(t *testing.T) {
	// 5112 时秒传兜底命中 → 返回秒传直链,不再透传错误。
	stubThumbDirect(t)
	origAnon := resolveAnonLink
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		return nil, err123TrafficLimit
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	origRapid := rapidShareTo123
	rapidShareTo123 = func(f File) *model.Link { return &model.Link{URL: "https://example.com/rapid-123"} }
	t.Cleanup(func() { rapidShareTo123 = origRapid })

	d := &Pan123Share{}
	file := File{FileName: "video.mp4"}

	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL != "https://example.com/rapid-123" {
		t.Fatalf("expected rapid fallback link, got %+v", link)
	}
}

// restore123Vars 还原 SaveTo 可替换桩。
func restore123Vars(task func(ctx context.Context, pan *_123.Pan123, data base.Json) (int64, error),
	wait func(ctx context.Context, pan *_123.Pan123, taskID int64) error,
	list func(ctx context.Context, pan *_123.Pan123, dir model.Obj) (map[string]struct{}, error)) func() {
	origTask, origWait, origList, origReuse := save123Task, wait123SaveTask, list123Target, open123ReuseTo
	save123Task, wait123SaveTask, list123Target = task, wait, list
	return func() {
		save123Task, wait123SaveTask, list123Target, open123ReuseTo = origTask, origWait, origList, origReuse
	}
}

// cookie 版目标:goapi copy/save,fileList 批量携带、目标目录落在每个条目的 parentFileID、
// 目录条目 type=1;新 id 经转存前后目标目录差集解析(任务响应不回报新 id)。
func TestPan123ShareSaveTo_CookieGoApi(t *testing.T) {
	var gotData base.Json
	calls := 0
	restore := restore123Vars(
		func(ctx context.Context, pan *_123.Pan123, data base.Json) (int64, error) {
			gotData = data
			return 53436542, nil
		},
		func(ctx context.Context, pan *_123.Pan123, taskID int64) error { return nil },
		func(ctx context.Context, pan *_123.Pan123, dir model.Obj) (map[string]struct{}, error) {
			calls++
			if calls == 1 {
				return map[string]struct{}{"old": {}}, nil
			}
			return map[string]struct{}{"old": {}, "9001": {}, "9002": {}}, nil
		})
	defer restore()

	d := &Pan123Share{Addition: Addition{ShareKey: "MPrAjv", SharePwd: "ab12"}}
	dst := &model.Object{ID: "32777979", Name: "我的追剧/剧名", IsFolder: true}
	objs := []model.Obj{
		File{FileName: "第01集.mkv", Size: 123, FileId: 101, Type: 0, Etag: "ABCD"},
		File{FileName: "剧名", FileId: 102, Type: 1},
	}

	saved, err := d.SaveTo(context.Background(), &_123.Pan123{}, dst, objs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotData["shareKey"] != "MPrAjv" || gotData["sharePwd"] != "ab12" {
		t.Errorf("share params mismatch: %v", gotData)
	}
	list, _ := gotData["fileList"].([]map[string]interface{})
	if len(list) != 2 {
		t.Fatalf("fileList len: got %d want 2", len(list))
	}
	if list[0]["fileID"] != int64(101) || list[0]["etag"] != "ABCD" || list[0]["type"] != 0 ||
		list[0]["parentFileID"] != int64(32777979) || list[0]["fileName"] != "第01集.mkv" || list[0]["driveID"] != 0 {
		t.Errorf("file entry mismatch: %v", list[0])
	}
	if list[1]["type"] != 1 || list[1]["parentFileID"] != int64(32777979) {
		t.Errorf("dir entry must carry type=1 and target parent: %v", list[1])
	}
	got := map[string]bool{}
	for _, id := range saved {
		got[id] = true
	}
	if !got["9001"] || !got["9002"] || got["old"] {
		t.Errorf("diff must return only new ids, got %v", saved)
	}
}

// 开放平台目标:按 Etag(MD5)秒传;目录对象回退字节中转。
func TestPan123ShareSaveTo_OpenRapid(t *testing.T) {
	var gotParent int64
	var gotHash, gotName string
	calls := 0
	restore := restore123Vars(nil, nil, nil)
	open123ReuseTo = func(open *_123_open.Open123, parentFileID int64, hash, filename string, size int64) (bool, int64, error) {
		calls++
		gotParent, gotHash, gotName = parentFileID, hash, filename
		return true, int64(9000 + calls), nil
	}
	defer restore()

	d := &Pan123Share{}
	dst := &model.Object{ID: "888", Name: "我的追剧/剧名", IsFolder: true}
	saved, err := d.SaveTo(context.Background(), &_123_open.Open123{}, dst,
		[]model.Obj{File{FileName: "第01集.mkv", Size: 123, FileId: 101, Etag: "ABCD"}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotParent != 888 || gotHash != "ABCD" || gotName != "第01集.mkv" {
		t.Errorf("reuse params mismatch: %d %s %s", gotParent, gotHash, gotName)
	}
	if len(saved) != 1 || saved[0] != "9001" {
		t.Errorf("saved ids: got %v want [9001]", saved)
	}

	_, err = d.SaveTo(context.Background(), &_123_open.Open123{}, dst,
		[]model.Obj{File{FileName: "剧名", FileId: 102, Type: 1}})
	if err == nil || !strings.Contains(err.Error(), "目录") {
		t.Fatalf("expected dir rejection on open target, got %v", err)
	}
}

// 任务提交失败错误上抛,由调用方回退字节中转 copy。
func TestPan123ShareSaveTo_TaskErrorPropagates(t *testing.T) {
	restore := restore123Vars(
		func(ctx context.Context, pan *_123.Pan123, data base.Json) (int64, error) {
			return 0, errors.New("空间不足")
		}, nil,
		func(ctx context.Context, pan *_123.Pan123, dir model.Obj) (map[string]struct{}, error) {
			return map[string]struct{}{}, nil
		})
	defer restore()

	d := &Pan123Share{}
	dst := &model.Object{ID: "1", Name: "剧", IsFolder: true}
	_, err := d.SaveTo(context.Background(), &_123.Pan123{}, dst,
		[]model.Obj{File{FileName: "第01集.mkv", Size: 1, FileId: 101}})
	if err == nil || !strings.Contains(err.Error(), "空间不足") {
		t.Fatalf("expected task error to propagate, got %v", err)
	}
}

// 目标存储不是 123 账号驱动时拒绝,不发起任何请求。
func TestPan123ShareSaveTo_RejectsNon123Target(t *testing.T) {
	called := false
	restore := restore123Vars(
		func(ctx context.Context, pan *_123.Pan123, data base.Json) (int64, error) {
			called = true
			return 0, nil
		}, nil, nil)
	defer restore()

	d := &Pan123Share{}
	dst := &model.Object{ID: "1", Name: "剧", IsFolder: true}
	_, err := d.SaveTo(context.Background(), nil, dst,
		[]model.Obj{File{FileName: "第01集.mkv", Size: 1, FileId: 101}})
	if err == nil || !strings.Contains(err.Error(), "123网盘账号") {
		t.Fatalf("expected non-123 target rejection, got %v", err)
	}
	if called {
		t.Fatal("rejection must not trigger any task")
	}
}
