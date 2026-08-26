package _115_share

import (
	"context"
	"errors"
	"fmt"
	_115 "github.com/OpenListTeam/OpenList/v4/drivers/115"
	_123rapid "github.com/OpenListTeam/OpenList/v4/drivers/123_rapid"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	driver115 "github.com/power721/115driver/pkg/driver"
	"golang.org/x/time/rate"
)

type Pan115Share struct {
	model.Storage
	Addition
	limiter *rate.Limiter
}

func (d *Pan115Share) Config() driver.Config {
	return config
}

func (d *Pan115Share) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Pan115Share) Init(ctx context.Context) error {
	//if d.LimitRate > 0 {
	//	d.limiter = rate.NewLimiter(rate.Limit(d.LimitRate), 1)
	//}

	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	return d.Validate()
}

func (d *Pan115Share) WaitLimit(ctx context.Context) error {
	if d.limiter != nil {
		return d.limiter.Wait(ctx)
	}
	return nil
}

func (d *Pan115Share) Drop(ctx context.Context) error {
	return nil
}

func (d *Pan115Share) Validate() error {
	pan115 := op.Get115Driver(idx)
	if pan115 == nil {
		return errors.New("找不到115云盘帐号")
	}
	client := pan115.(*_115.Pan115).GetClient()
	_, err := client.GetShareSnap(d.ShareCode, d.ReceiveCode, "0", driver115.QueryLimit(1))
	return err
}

func (d *Pan115Share) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	storage := op.Get115Driver(idx)
	if storage == nil {
		return []model.Obj{}, errors.New("找不到115云盘帐号")
	}
	pan115 := storage.(*_115.Pan115)
	if err := pan115.WaitLimit(ctx); err != nil {
		return nil, err
	}
	client := pan115.GetClient()

	files := make([]driver115.ShareFile, 0)
	fileResp, err := client.GetShareSnap(d.ShareCode, d.ReceiveCode, dir.GetID(), driver115.QueryLimit(int(pan115.PageSize)))
	if err != nil {
		return nil, err
	}
	files = append(files, fileResp.Data.List...)
	total := fileResp.Data.Count
	count := len(fileResp.Data.List)
	for total > count {
		fileResp, err := client.GetShareSnap(
			d.ShareCode, d.ReceiveCode, dir.GetID(),
			driver115.QueryLimit(int(pan115.PageSize)), driver115.QueryOffset(count),
		)
		if err != nil {
			return nil, err
		}
		files = append(files, fileResp.Data.List...)
		count += len(fileResp.Data.List)
	}

	return utils.SliceConvert(files, transFunc)
}

func (d *Pan115Share) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	// 123 优先:开关开启时先按 SHA1 秒传到 123,失败/未命中回退现有「转存个人 115」取链。
	if setting.GetBool(conf.Pan115To123) {
		if link := rapid115To123(ctx, file); link != nil {
			return link, nil
		}
	}
	count := op.GetDriverCount("115 Cloud")
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

// rapid115To123 把 115 分享文件按 SHA1 秒传到 123。声明为 var 便于单测替换。
var rapid115To123 = func(ctx context.Context, file model.Obj) *model.Link {
	parts := strings.Split(file.GetID(), "-")
	if len(parts) < 2 {
		return nil
	}
	link, err := _123rapid.RapidTo123(ctx, _123rapid.Source{
		HashType: utils.SHA1,
		Hash:     parts[1],
		Name:     file.GetName(),
		Size:     file.GetSize(),
	})
	if err != nil || link == nil {
		log.Debugf("[115-share] rapid to 123 skipped: %v", err)
		return nil
	}
	log.Infof("[115-share] 使用123秒传直链: %s", file.GetName())
	return link
}

func (d *Pan115Share) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	storage := op.Get115Driver(idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到115云盘帐号")
	}
	pan115 := storage.(*_115.Pan115)
	if err := pan115.WaitLimit(ctx); err != nil {
		return nil, err
	}
	client := pan115.GetClient()
	log.Infof("[%v] 获取115文件直链 %v %v %v", pan115.ID, file.GetName(), file.GetID(), file.GetSize())

	parts := strings.Split(file.GetID(), "-")
	fid := parts[0]
	sha1 := parts[1]
	downloadInfo, err := client.DownloadByShareCode(d.ShareCode, d.ReceiveCode, fid)
	if err != nil {
		return nil, err
	}
	go delayDelete115(pan115, sha1)
	exp := 4 * time.Hour
	header := http.Header{}
	header.Set("User-Agent", conf.UA115Browser)
	return &model.Link{
		URL:         downloadInfo.URL.URL + fmt.Sprintf("#storageId=%d", pan115.ID),
		Expiration:  &exp,
		Header:      header,
		Concurrency: pan115.Concurrency,
		PartSize:    pan115.ChunkSize * utils.KB,
	}, nil
}

func delayDelete115(pan115 *_115.Pan115, sha1 string) {
	delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
	if delayTime == 0 {
		return
	}

	log.Infof("[%v] Delete 115 temp file %v after %v seconds.", pan115.ID, sha1, delayTime)
	time.Sleep(time.Duration(delayTime) * time.Second)
	pan115.DeleteReceivedFile(sha1)
}

func (d *Pan115Share) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	return errs.NotSupport
}

func (d *Pan115Share) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotSupport
}

func (d *Pan115Share) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	return errs.NotSupport
}

func (d *Pan115Share) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotSupport
}

func (d *Pan115Share) Remove(ctx context.Context, obj model.Obj) error {
	return errs.NotSupport
}

func (d *Pan115Share) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	return errs.NotSupport
}

// shareReceiveURL 分享接收(转存)端点;声明为 var 便于单测替换(测试桩 httptest 服务器)。
var shareReceiveURL = "https://webapi.115.com/share/receive"

// listTargetIndex 列目标目录建索引(文件按 sha1、目录按名称 → fid);声明为 var 便于单测替换。
var listTargetIndex = func(client *driver115.Pan115Client, cid string) (map[string]string, error) {
	files, err := client.List(cid)
	if err != nil {
		return nil, err
	}
	index := make(map[string]string, len(*files))
	for _, f := range *files {
		key := "f:" + f.Sha1
		if f.IsDirectory {
			key = "d:" + f.Name
		}
		index[key] = f.FileID
	}
	return index, nil
}

type shareReceiveResp struct {
	State   bool   `json:"state"`
	Code    int    `json:"code"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

// SaveTo 把分享对象(文件或目录)服务端转存到 115 云盘账号(cookie 版)存储的目标目录,
// 实现 driver.ShareSaver 契约:webapi share/receive 的 cid 参数直达目标目录,不经服务器字节中转。
// 仅 cookie 版账号驱动可转存(开放平台无分享接收接口);目录对象由网盘侧整棵递归转存。
// 响应体不带新对象 fid 的稳定字段,新 id 经转存前后目标目录清单差集解析(文件按 sha1、目录按名称)。
func (d *Pan115Share) SaveTo(ctx context.Context, dstStorage driver.Driver, dstDir model.Obj, objs []model.Obj) ([]string, error) {
	pan115, ok := dstStorage.(*_115.Pan115)
	if !ok {
		return nil, errors.New("目标存储不是115云盘账号(cookie 版)驱动,不支持服务端转存")
	}
	return d.saveTo(ctx, pan115, pan115.GetClient(), dstDir, objs)
}

func (d *Pan115Share) saveTo(ctx context.Context, pan115 *_115.Pan115, client *driver115.Pan115Client, dstDir model.Obj, objs []model.Obj) ([]string, error) {
	if err := pan115.WaitLimit(ctx); err != nil {
		return nil, err
	}
	cid := dstDir.GetID()

	// 分享对象 id:文件为「fid-sha1」复合(Link 同款拆法),目录为裸 cid;file_id 参数只要 fid
	fids := make([]string, 0, len(objs))
	for _, obj := range objs {
		fids = append(fids, strings.SplitN(obj.GetID(), "-", 2)[0])
	}

	before, err := listTargetIndex(client, cid)
	if err != nil {
		log.Debugf("[115-share] list target dir before receive failed: %v", err)
		before = nil // 转存不阻断,只是事后解析不了新 id
	}

	result := shareReceiveResp{}
	referer := "https://115cdn.com/s/" + d.ShareCode + "?password=" + d.ReceiveCode + "&"
	resp, err := client.NewRequest().
		SetFormData(map[string]string{
			"share_code":   d.ShareCode,
			"receive_code": d.ReceiveCode,
			"file_id":      strings.Join(fids, ","),
			"cid":          cid,
		}).
		SetHeader("Referer", referer).
		SetResult(&result).
		ForceContentType("application/json;charset=UTF-8").
		Post(shareReceiveURL)
	if err != nil {
		return nil, err
	}
	if !result.State {
		msg := result.Error
		if msg == "" {
			msg = result.Message
		}
		if msg == "" {
			msg = resp.String()
		}
		return nil, errors.New(msg)
	}
	log.Infof("[%v] 115服务端转存 %d 个对象到 %v", pan115.ID, len(objs), dstDir.GetPath())

	if before == nil {
		return []string{}, nil
	}
	if err := pan115.WaitLimit(ctx); err != nil {
		return nil, err
	}
	after, err := listTargetIndex(client, cid)
	if err != nil {
		log.Debugf("[115-share] list target dir after receive failed: %v", err)
		return []string{}, nil
	}
	saved := make([]string, 0, len(objs))
	for key, fid := range after {
		if _, existed := before[key]; !existed {
			saved = append(saved, fid)
		}
	}
	return saved, nil
}

var (
	_ driver.Driver     = (*Pan115Share)(nil)
	_ driver.ShareSaver = (*Pan115Share)(nil)
)
