package _189_share

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_189pc "github.com/OpenListTeam/OpenList/v4/drivers/189pc"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

type Cloud189Share struct {
	model.Storage
	Addition
	client *resty.Client
}

var cloud189ShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)

var resolveCloud189ShareLink = func(ctx context.Context, d *Cloud189Share, file model.Obj) (*model.Link, error) {
	count := op.GetDriverCount("189CloudPC")
	var lastErr error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file)
		if err == nil {
			return link, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (d *Cloud189Share) Config() driver.Config {
	return config
}

func (d *Cloud189Share) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Cloud189Share) Init(ctx context.Context) error {
	d.client = base.NewRestyClient().SetHeaders(map[string]string{
		"Accept":  "application/json;charset=UTF-8",
		"Referer": "https://cloud.189.cn",
	})

	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	return d.Validate()
}

func (d *Cloud189Share) Drop(ctx context.Context) error {
	return nil
}

func (d *Cloud189Share) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.getShareFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	return utils.SliceConvert(files, func(src FileObj) (model.Obj, error) {
		src.Path = filepath.Join(dir.GetPath(), src.GetID())
		return &src, nil
	})
}

func (d *Cloud189Share) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	err := limiter.WaitN(ctx, 1)
	if err != nil {
		return nil, err
	}

	_, ok := file.(*FileObj)
	if !ok {
		return nil, errors.New("文件格式错误")
	}

	key := file.GetID()
	if link, ok := cloud189ShareLinkCache.Get(key); ok {
		return link, nil
	}

	link, err := resolveCloud189ShareLink(ctx, d, file)
	if err == nil && link != nil {
		cloud189ShareLinkCache.Set(key, link)
	}
	return link, err
}

func (d *Cloud189Share) link(ctx context.Context, file model.Obj) (*model.Link, error) {
	storage := op.GetFirstDriver("189CloudPC", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到天翼云盘帐号")
	}
	cloud189PC := storage.(*_189pc.Cloud189PC)
	log.Infof("[%v] 获取天翼云盘文件直链 %v %v %v", cloud189PC.ID, file.GetName(), file.GetID(), file.GetSize())

	shareInfo, err := d.getShareInfo()
	if err != nil {
		return nil, err
	}

	link, err := cloud189PC.GetShareLink(shareInfo.ShareId, file)
	if link != nil {
		return link, nil
	} else {
		log.Warnf("[%v] Get share link error: %v", cloud189PC.ID, err)
	}

	fileObject, _ := file.(*FileObj)
	log.Infof("[%v] 获取天翼云盘转存链接 %v %v", cloud189PC.ID, file.GetName(), file.GetID())
	link, err = cloud189PC.Transfer(ctx, shareInfo.ShareId, fileObject.ID, fileObject.oldName)
	return link, err
}

var _ driver.Driver = (*Cloud189Share)(nil)
var _ driver.ShareSaver = (*Cloud189Share)(nil)

// shareSaveTask 提交并等待 SHARE_SAVE 批量任务,返回转存成功的新对象 id(任务状态 successedFileIdList);
// 声明为 var 便于单测替换。同名冲突(target object conflict/状态 2)视为已完成。
var shareSaveTask = func(ctx context.Context, pc *_189pc.Cloud189PC, shareId int, targetFolderId string, infos []_189pc.BatchTaskInfo) ([]string, error) {
	resp, err := pc.CreateBatchTask("SHARE_SAVE", "", targetFolderId,
		map[string]string{"shareId": strconv.Itoa(shareId)}, infos...)
	if err != nil && !strings.Contains(err.Error(), "there is a conflict with the target object") {
		return nil, err
	}
	if resp == nil || resp.TaskID == "" {
		// 同名冲突时 create 即报错且无任务号:目标已有同名对象,按已完成处理
		return []string{}, nil
	}
	// 不复用 WaitBatchTask(无超时会挂死同步调用),自轮 CheckBatchTask 有界等待
	deadline := time.Now().Add(2 * time.Minute)
	for {
		state, err := pc.CheckBatchTask("SHARE_SAVE", resp.TaskID)
		if err != nil {
			return nil, err
		}
		switch state.TaskStatus {
		case 2, 4: // 2=同名冲突(已存在),4=完成
			ids := make([]string, 0, len(state.SuccessedFileIDList))
			for _, fid := range state.SuccessedFileIDList {
				ids = append(ids, strconv.FormatInt(fid, 10))
			}
			return ids, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("SHARE_SAVE 任务超时未完成")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// list189Target 目标目录清单 id 集(转存差集兜底解析);声明为 var 便于单测替换。
var list189Target = func(ctx context.Context, pc *_189pc.Cloud189PC, dir model.Obj) (map[string]struct{}, error) {
	files, err := pc.List(ctx, dir, model.ListArgs{})
	if err != nil {
		return nil, err
	}
	index := make(map[string]struct{}, len(files))
	for _, f := range files {
		index[f.GetID()] = struct{}{}
	}
	return index, nil
}

// SaveTo 把分享对象(文件或目录)服务端转存到天翼云盘账号(189CloudPC)存储的目标目录,
// 实现 driver.ShareSaver 契约:SHARE_SAVE 批量任务 targetFolderId 直达目标目录
// (与取链兜底 Transfer 同原语,那边硬编码临时目录);目录对象由网盘侧整棵递归转存。
// 新对象 id 取任务状态 successedFileIdList(目录转存可能不回报),空则回退转存前后目标目录差集。
func (d *Cloud189Share) SaveTo(ctx context.Context, dstStorage driver.Driver, dstDir model.Obj, objs []model.Obj) ([]string, error) {
	pc, ok := dstStorage.(*_189pc.Cloud189PC)
	if !ok {
		return nil, errors.New("目标存储不是天翼云盘账号(189CloudPC)驱动,不支持服务端转存")
	}
	shareInfo, err := d.getShareInfo()
	if err != nil {
		return nil, err
	}

	before, err := list189Target(ctx, pc, dstDir)
	if err != nil {
		log.Debugf("[%v] list target dir before share save failed: %v", d.ID, err)
		before = nil // 差集兜底不可用不阻断转存
	}

	infos := make([]_189pc.BatchTaskInfo, 0, len(objs))
	for _, obj := range objs {
		isFolder := 0
		if obj.IsDir() {
			isFolder = 1
		}
		infos = append(infos, _189pc.BatchTaskInfo{
			FileId:   obj.GetID(),
			FileName: obj.GetName(),
			IsFolder: isFolder,
		})
	}
	saved, err := shareSaveTask(ctx, pc, shareInfo.ShareId, dstDir.GetID(), infos)
	if err != nil {
		return nil, fmt.Errorf("SHARE_SAVE 转存 %d 个对象失败: %w", len(objs), err)
	}
	log.Infof("[%v] 189服务端转存 %d 个对象到 %v(shareId %v)", pc.ID, len(objs), dstDir.GetPath(), shareInfo.ShareId)

	if len(saved) > 0 || before == nil {
		return saved, nil
	}
	after, err := list189Target(ctx, pc, dstDir)
	if err != nil {
		log.Debugf("[%v] list target dir after share save failed: %v", d.ID, err)
		return saved, nil
	}
	for id := range after {
		if _, existed := before[id]; !existed {
			saved = append(saved, id)
		}
	}
	return saved, nil
}
