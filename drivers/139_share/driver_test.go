package _139_share

import (
	"context"
	"errors"
	"strings"
	"testing"

	_139 "github.com/OpenListTeam/OpenList/v4/drivers/139"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// SaveTo 服务端转存:文件/目录分列 contentInfoList/catalogInfoList,newCatalogID 直达目标目录。
func TestYun139ShareSaveTo(t *testing.T) {
	var gotContent, gotCatalog []string
	var gotParent, gotName string
	orig := save139Task
	save139Task = func(ctx context.Context, y *Yun139Share, yun139 *_139.Yun139, contentIDs, catalogIDs []string, newCatalogID, newCatalogName string) ([]string, error) {
		gotContent, gotCatalog, gotParent, gotName = contentIDs, catalogIDs, newCatalogID, newCatalogName
		return []string{"rst-1", "rst-2"}, nil
	}
	t.Cleanup(func() { save139Task = orig })

	d := &Yun139Share{}
	dst := &model.Object{ID: "Fi0pbscJ9lrt", Name: "我的追剧/剧名", IsFolder: true}

	saved, err := d.SaveTo(context.Background(), &_139.Yun139{}, dst, []model.Obj{
		&model.Object{ID: "co-1", Name: "第01集.mkv"},
		&model.Object{ID: "ca-1", Name: "剧名", IsFolder: true},
		&model.Object{ID: "co-2", Name: "第02集.mkv"},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Join(gotContent, ",") != "co-1,co-2" || strings.Join(gotCatalog, ",") != "ca-1" {
		t.Errorf("content/catalog split: got %v / %v", gotContent, gotCatalog)
	}
	if gotParent != "Fi0pbscJ9lrt" || gotName != "我的追剧/剧名" {
		t.Errorf("target dir params: got %q %q", gotParent, gotName)
	}
	if strings.Join(saved, ",") != "rst-1,rst-2" {
		t.Errorf("saved ids: got %v", saved)
	}
}

// 转存任务失败错误上抛,由调用方回退字节中转 copy。
func TestYun139ShareSaveTo_TaskErrorPropagates(t *testing.T) {
	orig := save139Task
	save139Task = func(ctx context.Context, y *Yun139Share, yun139 *_139.Yun139, contentIDs, catalogIDs []string, newCatalogID, newCatalogName string) ([]string, error) {
		return nil, errors.New("空间不足")
	}
	t.Cleanup(func() { save139Task = orig })

	d := &Yun139Share{}
	dst := &model.Object{ID: "1", Name: "剧名", IsFolder: true}
	_, err := d.SaveTo(context.Background(), &_139.Yun139{}, dst,
		[]model.Obj{&model.Object{ID: "co-1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "空间不足") {
		t.Fatalf("expected task error to propagate, got %v", err)
	}
}

// 目标存储不是移动云盘账号驱动时拒绝,不发起任何请求。
func TestYun139ShareSaveTo_RejectsNon139Target(t *testing.T) {
	called := false
	orig := save139Task
	save139Task = func(ctx context.Context, y *Yun139Share, yun139 *_139.Yun139, contentIDs, catalogIDs []string, newCatalogID, newCatalogName string) ([]string, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { save139Task = orig })

	d := &Yun139Share{}
	dst := &model.Object{ID: "1", Name: "剧名", IsFolder: true}
	_, err := d.SaveTo(context.Background(), nil, dst,
		[]model.Obj{&model.Object{ID: "co-1", Name: "第01集.mkv"}})
	if err == nil || !strings.Contains(err.Error(), "移动云盘账号") {
		t.Fatalf("expected non-139 target rejection, got %v", err)
	}
	if called {
		t.Fatal("rejection must not trigger any task")
	}
}
