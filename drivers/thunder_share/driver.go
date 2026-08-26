package thunder_share

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/drivers/thunder_browser"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

type ThunderShare struct {
	model.Storage
	Addition
}

var thunderShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)

var resolveThunderShareLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	count := op.GetDriverCount("ThunderBrowser")
	var lastErr error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file, args)
		if err == nil {
			return link, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (d *ThunderShare) Config() driver.Config {
	return config
}

func (d *ThunderShare) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *ThunderShare) Init(ctx context.Context) error {
	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	return d.Validate()
}

func (d *ThunderShare) Drop(ctx context.Context) error {
	return nil
}

func (d *ThunderShare) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.listShareFiles(ctx, dir)
	if err != nil {
		log.Warnf("list Thunder files error: %v", err)
		return nil, err
	}
	return files, err
}

func (d *ThunderShare) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if strings.HasSuffix(strings.ToLower(file.GetName()), ".cas") {
		return resolveThunderShareCASLink(ctx, d, file, args)
	}

	key := file.GetID()
	if link, ok := thunderShareLinkCache.Get(key); ok {
		return link, nil
	}

	link, err := resolveThunderShareLink(ctx, d, file, args)
	if err == nil && link != nil {
		thunderShareLinkCache.Set(key, link)
	}
	return link, err
}

func (d *ThunderShare) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	storage := op.GetFirstDriver("ThunderBrowser", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到迅雷云盘帐号")
	}
	thunder := storage.(*thunder_browser.ThunderBrowser)
	log.Infof("[%v] 获取迅雷云盘文件直链 %v %v %v", thunder.ID, file.GetName(), file.GetID(), file.GetSize())
	fileId, err := d.saveFile(ctx, thunder, file)
	if err != nil {
		log.Warnf("[%v] 保存迅雷文件失败: %v", thunder.ID, err)
		return nil, err
	}

	link, err := d.getDownloadUrl(ctx, thunder, fileId)
	return link, err
}

var _ driver.Driver = (*ThunderShare)(nil)
var _ driver.ShareSaver = (*ThunderShare)(nil)

// thunderShareRestore 提交 share/restore 转存请求(真实实现);声明为 var 便于单测替换。
var thunderShareRestore = func(ctx context.Context, thunder *thunder_browser.ThunderBrowser, data base.Json) (thunderShareRestoreResponse, error) {
	var restoreResp thunderShareRestoreResponse
	_, err := thunder.Request(SHARE_RESTORE_API_URL, http.MethodPost, func(r *resty.Request) {
		r.SetContext(ctx)
		r.SetBody(data)
	}, &restoreResp)
	return restoreResp, err
}

// SaveTo 把分享对象(文件或目录)服务端转存到迅雷云盘账号(ThunderBrowser)存储的目标目录,
// 实现 driver.ShareSaver 契约:share/restore 的 parent_id 直达目标目录、file_ids 一次批量携带
// (与取链兜底 saveFile 同原语,那边硬编码临时目录);目录对象由网盘侧整棵递归转存。
// 新对象 id 取响应 params.trace_file_ids 的「源 id → 新 id」映射。
func (d *ThunderShare) SaveTo(ctx context.Context, dstStorage driver.Driver, dstDir model.Obj, objs []model.Obj) ([]string, error) {
	thunder, ok := dstStorage.(*thunder_browser.ThunderBrowser)
	if !ok {
		return nil, errors.New("目标存储不是迅雷云盘账号(ThunderBrowser)驱动,不支持服务端转存")
	}
	if d.ShareToken == "" {
		// 空 pass_code_token 是「提取码错误/分享被屏蔽」的报错信号(getShareInfo 内已防护)
		if _, err := d.getShareInfo(ctx, thunder); err != nil {
			return nil, err
		}
	}

	ids := make([]string, 0, len(objs))
	for _, obj := range objs {
		ids = append(ids, obj.GetID())
	}
	data := base.Json{
		"file_ids":          ids,
		"ancestor_ids":      []string{},
		"parent_id":         dstDir.GetID(),
		"share_id":          d.ShareId,
		"pass_code_token":   d.ShareToken,
		"specify_parent_id": true,
	}
	restoreResp, err := thunderShareRestore(ctx, thunder, data)
	if err != nil {
		return nil, fmt.Errorf("share/restore 转存 %d 个对象失败: %w", len(objs), err)
	}
	saved := make([]string, 0, len(ids))
	for _, id := range ids {
		if newID, ok := restoreResp.RestoredFileID(id); ok {
			saved = append(saved, newID)
		}
	}
	log.Infof("[%v] 迅雷服务端转存 %d 个对象到 %v", thunder.ID, len(objs), dstDir.GetPath())
	return saved, nil
}
