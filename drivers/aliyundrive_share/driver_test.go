package aliyundrive_share

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/drivers/aliyundrive_open"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// SaveTo 服务端转存:逐对象 file/copy,目标目录透传 to_parent_file_id,新 id 收集自响应。
func TestAliyundriveShareSaveTo(t *testing.T) {
	type call struct {
		fileID, parent string
	}
	var calls []call
	orig := aliShareCopyOne
	aliShareCopyOne = func(ctx context.Context, d *AliyundriveShare, ali *aliyundrive_open.AliyundriveOpen, fileId, parentFileID string) (string, error) {
		calls = append(calls, call{fileId, parentFileID})
		return "new-" + fileId, nil
	}
	t.Cleanup(func() { aliShareCopyOne = orig })

	d := &AliyundriveShare{}
	d.ShareToken = "ST"
	dst := &model.Object{ID: "target-dir", Name: "我的追剧/剧名", IsFolder: true}

	saved, err := d.SaveTo(context.Background(), &aliyundrive_open.AliyundriveOpen{}, dst, []model.Obj{
		&model.Object{ID: "f1", Name: "第01集.mkv"},
		&model.Object{ID: "dir1", Name: "剧名", IsFolder: true},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(calls) != 2 || calls[0].fileID != "f1" || calls[1].fileID != "dir1" {
		t.Errorf("copy calls: %+v", calls)
	}
	for _, c := range calls {
		if c.parent != "target-dir" {
			t.Errorf("parent must be target dir, got %q", c.parent)
		}
	}
	if strings.Join(saved, ",") != "new-f1,new-dir1" {
		t.Errorf("saved ids: got %v", saved)
	}
}

// 单对象失败错误上抛,由调用方回退字节中转 copy。
func TestAliyundriveShareSaveTo_ErrorPropagates(t *testing.T) {
	orig := aliShareCopyOne
	aliShareCopyOne = func(ctx context.Context, d *AliyundriveShare, ali *aliyundrive_open.AliyundriveOpen, fileId, parentFileID string) (string, error) {
		return "", errors.New("share canceled")
	}
	t.Cleanup(func() { aliShareCopyOne = orig })

	d := &AliyundriveShare{}
	d.ShareToken = "ST"
	dst := &model.Object{ID: "1", Name: "剧名", IsFolder: true}
	_, err := d.SaveTo(context.Background(), &aliyundrive_open.AliyundriveOpen{}, dst,
		[]model.Obj{&model.Object{ID: "f1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "share canceled") {
		t.Fatalf("expected copy error to propagate, got %v", err)
	}
}

// 目标存储不是阿里账号驱动时拒绝,不发起任何请求。
func TestAliyundriveShareSaveTo_RejectsNonAliTarget(t *testing.T) {
	called := false
	orig := aliShareCopyOne
	aliShareCopyOne = func(ctx context.Context, d *AliyundriveShare, ali *aliyundrive_open.AliyundriveOpen, fileId, parentFileID string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { aliShareCopyOne = orig })

	d := &AliyundriveShare{}
	dst := &model.Object{ID: "1", Name: "剧名", IsFolder: true}
	_, err := d.SaveTo(context.Background(), nil, dst,
		[]model.Obj{&model.Object{ID: "f1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "阿里云盘账号") {
		t.Fatalf("expected non-ali target rejection, got %v", err)
	}
	if called {
		t.Fatal("rejection must not trigger any copy request")
	}
}
