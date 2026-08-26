package _139_share

import (
	"context"
	"errors"
	"fmt"
	_139 "github.com/OpenListTeam/OpenList/v4/drivers/139"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
	"time"
)

type Yun139Share struct {
	model.Storage
	Addition
}

func (d *Yun139Share) Config() driver.Config {
	return config
}

func (d *Yun139Share) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Yun139Share) Init(ctx context.Context) error {
	return nil
}

func (d *Yun139Share) Drop(ctx context.Context) error {
	return nil
}

func (d *Yun139Share) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.list(dir.GetID())
	if err != nil {
		log.Warnf("list files error: %v", err)
		return nil, err
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return fileToObj(src), nil
	})
}

func (d *Yun139Share) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	count := op.GetDriverCount("139Yun")
	if count == 0 {
		return nil, errors.New("找不到移动云盘帐号")
	}
	var (
		link *model.Link
		err  error
	)
	for i := 0; i < count; i++ {
		link, err = d.myLink(ctx, file, args)
		if err == nil {
			return link, nil
		}
	}
	return nil, err
}

func (d *Yun139Share) myLink(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	storage := op.GetFirstDriver("139Yun", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到移动云盘帐号")
	}
	yun139 := storage.(*_139.Yun139)
	log.Infof("[%v] 获取移动云盘文件直链 %v %v %v", yun139.ID, file.GetName(), file.GetID(), file.GetSize())
	url, err := d.link(yun139, file.GetID())
	if err != nil {
		return nil, err
	}
	exp := 15 * time.Minute
	return &model.Link{
		URL:         url + fmt.Sprintf("#storageId=%d", yun139.ID),
		Expiration:  &exp,
		Concurrency: yun139.Concurrency,
		PartSize:    yun139.ChunkSize,
	}, nil
}

var _ driver.Driver = (*Yun139Share)(nil)

var _ driver.ShareSaver = (*Yun139Share)(nil)

// SaveTo 把分享对象(文件或目录)服务端转存到移动云盘账号(139Yun)存储的目标目录,
// 实现 driver.ShareSaver 契约:网页端「保存至云盘」同款 IBatchOprTask/createOuterLinkBatchOprTask
// (加密信封与取链同款),文件/目录分列 contentInfoList/catalogInfoList 一次批量提交、目录整棵递归,
// newCatalogID 直达目标目录;提交后轮询 queryBatchOprTaskDetail 至 taskStatus=2,
// 新对象 id 取任务明细 idRspInfo 的 srcId→rstId 映射(reason=0000)。
func (d *Yun139Share) SaveTo(ctx context.Context, dstStorage driver.Driver, dstDir model.Obj, objs []model.Obj) ([]string, error) {
	yun139, ok := dstStorage.(*_139.Yun139)
	if !ok {
		return nil, errors.New("目标存储不是移动云盘账号(139Yun)驱动,不支持服务端转存")
	}
	contentIDs := make([]string, 0, len(objs))
	catalogIDs := make([]string, 0, len(objs))
	for _, obj := range objs {
		if obj.IsDir() {
			catalogIDs = append(catalogIDs, obj.GetID())
		} else {
			contentIDs = append(contentIDs, obj.GetID())
		}
	}
	saved, err := save139Task(ctx, d, yun139, contentIDs, catalogIDs, dstDir.GetID(), dstDir.GetName())
	if err != nil {
		return nil, fmt.Errorf("外链转存 %d 个对象失败: %w", len(objs), err)
	}
	log.Infof("[%v] 139服务端转存 %d 个对象到 %v", yun139.ID, len(objs), dstDir.GetPath())
	return saved, nil
}
