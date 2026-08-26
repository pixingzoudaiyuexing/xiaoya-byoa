package _123Share

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/op"

	"golang.org/x/time/rate"

	_123 "github.com/OpenListTeam/OpenList/v4/drivers/123"
	_123_open "github.com/OpenListTeam/OpenList/v4/drivers/123_open"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

type Pan123Share struct {
	model.Storage
	Addition
	apiRateLimit sync.Map
	ref          *_123.Pan123
}

func (d *Pan123Share) Config() driver.Config {
	return config
}

func (d *Pan123Share) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Pan123Share) Init(ctx context.Context) error {
	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}
	return d.Validate()
}

func (d *Pan123Share) InitReference(storage driver.Driver) error {
	refStorage, ok := storage.(*_123.Pan123)
	if ok {
		d.ref = refStorage
		return nil
	}
	return fmt.Errorf("ref: storage is not 123Pan")
}

func (d *Pan123Share) Drop(ctx context.Context) error {
	d.ref = nil
	return nil
}

func (d *Pan123Share) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	// TODO return the files list, required
	files, err := d.getFiles(ctx, dir.GetID())
	if err != nil {
		return nil, err
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return src, nil
	})
}

// resolveAnonLink 匿名换链入口,声明为 var 以便单测替换(规避真实网络/op 依赖)。
var resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
	return d.anonDownloadLink(f, ip)
}

func (d *Pan123Share) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if f, ok := file.(File); ok {
		// 1) 无限直链:直接用 /share/get 列表里已签名的 DownloadUrl,剥掉缩略图变换标记。
		//    免登录、不转存、不占分享方提取流量与浏览者额度(见 unlimited.go)。
		if !d.DisableUnlimited {
			if link, err := resolveThumbDirect(d, ctx, f, args.IP); err == nil {
				return link, nil
			} else {
				log.Debugf("[123_share] 无限直链不可用,回退 download/info: %v", err)
			}
		}
		// 2) 匿名 share/download/info:同样免登录,但会记到分享方的提取流量上。
		if link, err := resolveAnonLink(d, f, args.IP); err == nil {
			return link, nil
		} else if errors.Is(err, err123TrafficLimit) {
			// 分享方流量耗尽:按 MD5(Etag)秒传到 123 Open 账号走个人下载(不走分享流量),
			// 参考 drivers/123_rapid/rapid.go;失败则透传真实错误。
			if link := rapidShareTo123(f); link != nil {
				return link, nil
			}
			return nil, err
		}
	}
	// 3) 最后才用配置的 123Pan 账号(鉴权 share/download/info)。
	count := op.GetDriverCount("123Pan")
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

func (d *Pan123Share) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	storage := op.GetFirstDriver("123Pan", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到123云盘帐号")
	}
	pan123 := storage.(*_123.Pan123)
	log.Infof("[%v] 获取123文件直链 %v %v %v", pan123.ID, file.GetName(), file.GetID(), file.GetSize())
	f, ok := file.(File)
	if !ok {
		return nil, fmt.Errorf("can't convert obj")
	}
	var headers map[string]string
	if !utils.IsLocalIPAddr(args.IP) {
		headers = map[string]string{
			"X-Forwarded-For": args.IP,
		}
	}
	data := base.Json{
		"driveId":   "0",
		"shareKey":  d.ShareKey,
		"SharePwd":  d.SharePwd,
		"etag":      f.Etag,
		"fileId":    f.FileId,
		"s3keyFlag": f.S3KeyFlag,
		"FileName":  f.FileName,
		"size":      f.Size,
	}
	resp, err := pan123.Request(DownloadInfo, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetHeaders(headers)
	}, nil)
	if err != nil {
		return nil, err
	}
	return unwrap123DownloadLink(utils.Json.Get(resp, "data", "DownloadURL").ToString())
}

func (d *Pan123Share) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	// TODO create folder, optional
	return errs.NotSupport
}

func (d *Pan123Share) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	// TODO move obj, optional
	return errs.NotSupport
}

func (d *Pan123Share) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	// TODO rename obj, optional
	return errs.NotSupport
}

func (d *Pan123Share) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	// TODO copy obj, optional
	return errs.NotSupport
}

func (d *Pan123Share) Remove(ctx context.Context, obj model.Obj) error {
	// TODO remove obj, optional
	return errs.NotSupport
}

func (d *Pan123Share) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	// TODO upload file, optional
	return errs.NotSupport
}

//func (d *Pan123Share) Other(ctx context.Context, args model.OtherArgs) (interface{}, error) {
//	return nil, errs.NotSupport
//}

func (d *Pan123Share) APIRateLimit(ctx context.Context, api string) error {
	value, _ := d.apiRateLimit.LoadOrStore(api,
		rate.NewLimiter(rate.Every(700*time.Millisecond), 1))
	limiter := value.(*rate.Limiter)

	return limiter.Wait(ctx)
}

var _ driver.Driver = (*Pan123Share)(nil)
var _ driver.ShareSaver = (*Pan123Share)(nil)

const (
	// goapi 转存端点(浏览器「保存到网盘」同款,分享站与 yun 主域同网关;签名走 GetApi/signPath)
	save123URL     = "https://yun.123pan.com/api/restful/goapi/v1/file/copy/save"
	save123TaskURL = "https://yun.123pan.com/api/restful/goapi/v1/file/copy/save/get"
)

// save123Task 提交转存任务(真实实现);声明为 var 便于单测替换。
var save123Task = func(ctx context.Context, pan *_123.Pan123, data base.Json) (int64, error) {
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			TaskID int64 `json:"taskID"`
		} `json:"data"`
	}
	_, err := pan.Request(_123.GetApi(save123URL), http.MethodPost, func(req *resty.Request) {
		req.SetContext(ctx).SetBody(data)
	}, &resp)
	if err != nil {
		return 0, err
	}
	if resp.Code != 0 {
		return 0, errors.New(resp.Message)
	}
	return resp.Data.TaskID, nil
}

// wait123SaveTask 轮询任务直至完成(真实实现);声明为 var 便于单测替换。
// 实测形态:完成后 status=2 且 errorCode=0;errorCode!=0 失败(reason 带原因)。
var wait123SaveTask = func(ctx context.Context, pan *_123.Pan123, taskID int64) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		var resp struct {
			Code int `json:"code"`
			Data struct {
				Status    int    `json:"status"`
				ErrorCode int    `json:"errorCode"`
				Reason    string `json:"reason"`
			} `json:"data"`
		}
		_, err := pan.Request(_123.GetApi(save123TaskURL+"?taskID="+strconv.FormatInt(taskID, 10)),
			http.MethodGet, func(req *resty.Request) {
				req.SetContext(ctx)
			}, &resp)
		if err != nil {
			return err
		}
		if resp.Data.ErrorCode != 0 {
			return fmt.Errorf("转存任务失败(%d): %s", resp.Data.ErrorCode, resp.Data.Reason)
		}
		if resp.Data.Status == 2 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("转存任务超时未完成")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// list123Target 目标目录 id 集(转存差集解析新 id);声明为 var 便于单测替换。
var list123Target = func(ctx context.Context, pan *_123.Pan123, dir model.Obj) (map[string]struct{}, error) {
	files, err := pan.List(ctx, dir, model.ListArgs{})
	if err != nil {
		return nil, err
	}
	index := make(map[string]struct{}, len(files))
	for _, f := range files {
		index[f.GetID()] = struct{}{}
	}
	return index, nil
}

// open123ReuseTo 123 Open 账号按 MD5(Etag)秒传到指定目录(真实实现);声明为 var 便于单测替换。
var open123ReuseTo = func(open *_123_open.Open123, parentFileID int64, hash, filename string, size int64) (bool, int64, error) {
	return open.ReuseTo(parentFileID, utils.MD5, hash, filename, size, 1)
}

// SaveTo 把分享对象(文件或目录)服务端转存到 123 网盘账号存储的目标目录,实现 driver.ShareSaver 契约:
//   - cookie 版(123Pan):分享站「保存到网盘」同款 goapi copy/save,fileList 批量携带、目标目录 =
//     fileList[].parentFileID、目录对象网盘侧整棵递归转存;提交后轮询 copy/save/get 任务直至完成,
//     新对象 id 经转存前后目标目录差集解析(任务响应不回报新 id)。
//   - 开放平台版(123 Open):按分享文件 Etag(MD5)纯 hash 秒传(file/create)直达目标目录;
//     目录无 hash、秒传未命中(123 服务端无同 hash 文件)均回退字节中转 copy。
func (d *Pan123Share) SaveTo(ctx context.Context, dstStorage driver.Driver, dstDir model.Obj, objs []model.Obj) ([]string, error) {
	switch dst := dstStorage.(type) {
	case *_123.Pan123:
		return d.saveToCookie(ctx, dst, dstDir, objs)
	case *_123_open.Open123:
		return d.saveToOpen(ctx, dst, dstDir, objs)
	default:
		return nil, errors.New("目标存储不是123网盘账号(123Pan/123 Open)驱动,不支持服务端转存")
	}
}

func (d *Pan123Share) saveToCookie(ctx context.Context, pan *_123.Pan123, dstDir model.Obj, objs []model.Obj) ([]string, error) {
	parentFileID, err := strconv.ParseInt(dstDir.GetID(), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("目标目录 id 非法: %w", err)
	}

	before, err := list123Target(ctx, pan, dstDir)
	if err != nil {
		log.Debugf("[123_share] list target dir before save failed: %v", err)
		before = nil
	}

	fileList := make([]map[string]interface{}, 0, len(objs))
	for _, obj := range objs {
		f, ok := obj.(File)
		if !ok {
			return nil, errors.New("can't convert obj")
		}
		fileType := 0
		if f.IsDir() {
			fileType = 1
		}
		fileList = append(fileList, map[string]interface{}{
			"fileID":       f.FileId,
			"size":         f.Size,
			"etag":         f.Etag,
			"type":         fileType,
			"parentFileID": parentFileID,
			"fileName":     f.FileName,
			"driveID":      0,
		})
	}
	data := base.Json{
		"fileList":     fileList,
		"shareKey":     d.ShareKey,
		"currentLevel": 0,
		"superAdmin":   nil,
	}
	if d.SharePwd != "" {
		data["sharePwd"] = d.SharePwd
	}

	taskID, err := save123Task(ctx, pan, data)
	if err != nil {
		return nil, fmt.Errorf("copy/save 提交失败: %w", err)
	}
	if err := wait123SaveTask(ctx, pan, taskID); err != nil {
		return nil, err
	}
	log.Infof("[%v] 123服务端转存 %d 个对象到 %v(task %d)", pan.ID, len(objs), dstDir.GetPath(), taskID)

	if before == nil {
		return []string{}, nil
	}
	after, err := list123Target(ctx, pan, dstDir)
	if err != nil {
		log.Debugf("[123_share] list target dir after save failed: %v", err)
		return []string{}, nil
	}
	saved := make([]string, 0, len(objs))
	for id := range after {
		if _, existed := before[id]; !existed {
			saved = append(saved, id)
		}
	}
	return saved, nil
}

func (d *Pan123Share) saveToOpen(ctx context.Context, open *_123_open.Open123, dstDir model.Obj, objs []model.Obj) ([]string, error) {
	parentFileID, err := strconv.ParseInt(dstDir.GetID(), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("目标目录 id 非法: %w", err)
	}
	saved := make([]string, 0, len(objs))
	for _, obj := range objs {
		f, ok := obj.(File)
		if !ok {
			return saved, errors.New("can't convert obj")
		}
		if f.IsDir() {
			return saved, fmt.Errorf("目录不支持秒传转存: %s(整目录回退字节中转)", f.FileName)
		}
		if f.Etag == "" || f.Size <= 0 {
			return saved, fmt.Errorf("分享文件缺少 Etag/Size,无法秒传: %s", f.FileName)
		}
		reuse, fileID, err := open123ReuseTo(open, parentFileID, f.Etag, f.FileName, f.Size)
		if err != nil {
			return saved, fmt.Errorf("秒传 %s 失败: %w", f.FileName, err)
		}
		if !reuse {
			return saved, fmt.Errorf("秒传未命中(123 服务端无同 hash 文件,回退字节中转): %s", f.FileName)
		}
		saved = append(saved, strconv.FormatInt(fileID, 10))
	}
	log.Infof("[%v] 123秒传转存 %d 个文件到 %v", open.ID, len(objs), dstDir.GetPath())
	return saved, nil
}
