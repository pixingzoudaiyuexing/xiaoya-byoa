package _115_share

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_115 "github.com/OpenListTeam/OpenList/v4/drivers/115"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	driver115 "github.com/power721/115driver/pkg/driver"
)

// restoreVars 还原可替换桩函数与端点。
func restoreVars(url string, list func(client *driver115.Pan115Client, cid string) (map[string]string, error)) func() {
	origURL, origList := shareReceiveURL, listTargetIndex
	shareReceiveURL, listTargetIndex = url, list
	return func() {
		shareReceiveURL, listTargetIndex = origURL, origList
	}
}

// saveTo 服务端转存:复合 id(file 是 fid-sha1、dir 是裸 cid)拆出 fid 逗号拼接、cid 直达目标目录、
// Referer 带 share_code/receive_code;新对象 id 由转存前后目录清单差集解析。
func TestPan115ShareSaveTo(t *testing.T) {
	var gotForm, gotReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = strings.Join([]string{
			"share_code=" + r.Form.Get("share_code"),
			"receive_code=" + r.Form.Get("receive_code"),
			"file_id=" + r.Form.Get("file_id"),
			"cid=" + r.Form.Get("cid"),
		}, ";")
		gotReferer = r.Header.Get("Referer")
		_, _ = w.Write([]byte(`{"state":true,"code":0,"message":""}`))
	}))
	defer srv.Close()

	calls := 0
	restore := restoreVars(srv.URL, func(client *driver115.Pan115Client, cid string) (map[string]string, error) {
		calls++
		if calls == 1 { // 转存前:已有第 1 集
			return map[string]string{"f:sha1-ep1": "fid1"}, nil
		}
		// 转存后:新增第 2 集与整目录
		return map[string]string{"f:sha1-ep1": "fid1", "f:sha1-ep2": "fid2", "d:剧名": "dirfid"}, nil
	})
	defer restore()

	d := &Pan115Share{}
	// share.Addition 字段:ShareCode/ReceiveCode —— 经嵌入 Addition 直接赋值
	d.ShareCode = "SC123"
	d.ReceiveCode = "RC456"
	dst := &model.Object{ID: "987654", Name: "我的追剧/剧名", IsFolder: true}

	saved, err := d.saveTo(context.Background(), &_115.Pan115{}, driver115.New(), dst,
		[]string{"111-sha1-ep1", "222-sha1-ep2", "333"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if want := "share_code=SC123;receive_code=RC456;file_id=111,222,333;cid=987654"; gotForm != want {
		t.Errorf("receive form: got %q want %q", gotForm, want)
	}
	if !strings.HasPrefix(gotReferer, "https://115cdn.com/s/SC123?password=RC456") {
		t.Errorf("referer must carry share/receive codes, got %q", gotReferer)
	}
	gotSet := map[string]bool{}
	for _, fid := range saved {
		gotSet[fid] = true
	}
	if !gotSet["fid2"] || !gotSet["dirfid"] || gotSet["fid1"] {
		t.Errorf("diff must return only new ids (fid2,dirfid), got %v", saved)
	}
}

// 转存接口报错(state=false)时错误上抛,由调用方回退字节中转 copy。
func TestPan115ShareSaveTo_ApiErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":false,"code":110001,"error":"分享已取消"}`))
	}))
	defer srv.Close()

	restore := restoreVars(srv.URL, func(client *driver115.Pan115Client, cid string) (map[string]string, error) {
		return nil, nil // before 列举失败不阻断转存
	})
	defer restore()

	d := &Pan115Share{}
	dst := &model.Object{ID: "1", Name: "剧名", IsFolder: true}
	_, err := d.saveTo(context.Background(), &_115.Pan115{}, driver115.New(), dst, []string{"111-sha1"})
	if err == nil || !strings.Contains(err.Error(), "分享已取消") {
		t.Fatalf("expected api error to propagate, got %v", err)
	}
}

// 目标存储不是 115 cookie 账号(开放平台/其它盘)时拒绝,不发起任何请求。
func TestPan115ShareSaveTo_RejectsNon115Target(t *testing.T) {
	called := false
	restore := restoreVars("http://must-not-be-called", func(client *driver115.Pan115Client, cid string) (map[string]string, error) {
		called = true
		return nil, nil
	})
	defer restore()

	d := &Pan115Share{}
	dst := &model.Object{ID: "1", Name: "剧名", IsFolder: true}
	_, err := d.SaveTo(context.Background(), nil, dst, []string{"111-sha1"})
	if err == nil || !strings.Contains(err.Error(), "115云盘账号") {
		t.Fatalf("expected non-115 target rejection, got %v", err)
	}
	if called {
		t.Fatal("rejection must not trigger any listing")
	}
}
