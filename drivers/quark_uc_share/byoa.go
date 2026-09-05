package quark_uc_share

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

// byoaDirectLink 使用当前浏览器提供的夸克 Cookie 直接对分享文件换取播放直链。
// 该路径不使用服务器账号池、不转存文件、不使用全局播放链接缓存。
func (d *QuarkUCShare) byoaDirectLink(ctx context.Context, file model.Obj, credential string) (*model.Link, error) {
	parts := strings.SplitN(file.GetID(), "-", 3)
	if len(parts) < 2 {
		return nil, errors.New("invalid share file id: " + file.GetID())
	}
	fileID, fidToken := parts[0], parts[1]
	parentID := ""
	if len(parts) == 3 {
		parentID = parts[2]
	}

	// Storage 中的 stoken 可作为公开目录已有值使用；如果需要刷新，新的 stoken 只保留在本请求局部变量。
	// BYOA 绝不把由某个访客 Cookie 换出的 token 写回 Driver/Storage，避免 A 请求改变 B 的全局状态。
	shareToken := d.ShareToken
	if shareToken == "" {
		var err error
		shareToken, err = d.byoaRefreshShareToken(ctx, credential)
		if err != nil {
			return nil, err
		}
	}

	body := base.Json{
		"fids":            []string{fileID},
		"fids_token":      []string{fidToken},
		"pwd_id":          d.ShareId,
		"stoken":          shareToken,
		"speedup_session": "",
	}

	requestDownload := func() (*DownResp, error) {
		var resp DownResp
		_, err := d.byoaRequestAt(ctx, d.pcApi(), credential, "/file/download", http.MethodPost, func(req *resty.Request) {
			req.SetBody(body)
		}, &resp)
		return &resp, err
	}

	resp, err := requestDownload()
	if err != nil && strings.Contains(err.Error(), "token校验异常") && parentID != "" {
		if newToken, tokenErr := d.byoaGetFileToken(ctx, credential, shareToken, parentID, fileID); tokenErr == nil && newToken != "" {
			body["fids_token"] = []string{newToken}
			resp, err = requestDownload()
		}
	}

	if err != nil && strings.Contains(err.Error(), "st invalid") {
		if refreshed, refreshErr := d.byoaRefreshShareToken(ctx, credential); refreshErr == nil {
			shareToken = refreshed
			body["stoken"] = shareToken
			resp, err = requestDownload()
		}
	}

	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Data) == 0 || resp.Data[0].DownloadUrl == "" {
		// 空直链时做一次最小自愈：刷新请求内分享 token，并在可用时刷新文件 token。
		if refreshed, refreshErr := d.byoaRefreshShareToken(ctx, credential); refreshErr == nil {
			shareToken = refreshed
			body["stoken"] = shareToken
			if parentID != "" {
				if newToken, tokenErr := d.byoaGetFileToken(ctx, credential, shareToken, parentID, fileID); tokenErr == nil && newToken != "" {
					body["fids_token"] = []string{newToken}
				}
			}
			resp, err = requestDownload()
		}
	}
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Data) == 0 || resp.Data[0].DownloadUrl == "" {
		return nil, errors.New("empty share download url")
	}

	downloadURL := resp.Data[0].DownloadUrl
	log.Infof("[BYOA][Quark] 获取免转存直链 %v %v", file.GetName(), file.GetSize())
	return &model.Link{
		URL: downloadURL,
		Header: http.Header{
			"User-Agent": []string{d.conf.ua},
			"Referer":    []string{d.conf.referer},
			"Cookie":     []string{credential},
		},
		Concurrency: 16,
		PartSize:    512 * utils.KB,
	}, nil
}

// byoaRefreshShareToken 使用当前浏览器 Cookie 刷新分享级 stoken。
// 返回值只用于当前请求，不写回 d.ShareToken 或 Storage，确保不同浏览器请求之间没有 BYOA 状态污染。
func (d *QuarkUCShare) byoaRefreshShareToken(ctx context.Context, credential string) (string, error) {
	data := base.Json{
		"pwd_id":             d.ShareId,
		"passcode":           d.SharePwd,
		"share_for_transfer": true,
	}
	var resp ShareTokenResp
	_, err := d.byoaRequestAt(ctx, d.pcApi(), credential, "/share/sharepage/token", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, &resp)
	if err != nil {
		return "", err
	}
	if resp.Data.ShareToken == "" {
		if resp.Message != "" {
			return "", errors.New(resp.Message)
		}
		return "", errors.New("empty share token")
	}
	return resp.Data.ShareToken, nil
}

// byoaGetFileToken 用当前浏览器 Cookie 和当前请求自己的 stoken 从分享目录重新取得单个文件的 fid token。
func (d *QuarkUCShare) byoaGetFileToken(ctx context.Context, credential, shareToken, parentID, fileID string) (string, error) {
	page := 1
	for {
		query := map[string]string{
			"pr":            d.conf.pr,
			"fr":            "pc",
			"pwd_id":        d.ShareId,
			"stoken":        shareToken,
			"pdir_fid":      parentID,
			"force":         "0",
			"_page":         strconv.Itoa(page),
			"_size":         "50",
			"_fetch_banner": "0",
			"_fetch_share":  "0",
			"_fetch_total":  "1",
			"_sort":         "file_type:asc," + d.OrderBy + ":" + d.OrderDirection,
		}
		var resp ListResp
		_, err := d.byoaRequestAt(ctx, d.pcApi(), credential, "/share/sharepage/detail", http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(query)
		}, &resp)
		if err != nil {
			return "", err
		}
		for _, f := range resp.Data.Files {
			if f.ID == fileID {
				return f.FID, nil
			}
		}
		if len(resp.Data.Files) == 0 || page*50 >= resp.Metadata.Total {
			break
		}
		page++
	}
	return "", errors.New("file not found")
}

// quarkBYOAAuthExpired 只识别明确的“当前浏览器登录已失效”信号。
// 分享 stoken、文件 token、限流等错误不能误判成账号过期，否则会无意义地反复弹扫码。
func quarkBYOAAuthExpired(httpStatus int, apiErr Resp) bool {
	if httpStatus == http.StatusUnauthorized || apiErr.Status == http.StatusUnauthorized || apiErr.Code == http.StatusUnauthorized {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(apiErr.Message))
	if message == "" {
		return false
	}
	markers := []string{
		"未登录",
		"请登录",
		"登录失效",
		"登录已失效",
		"登录过期",
		"登录已过期",
		"not login",
		"not logged",
		"login expired",
		"login invalid",
		"cookie expired",
		"cookie invalid",
	}
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// byoaRequestAt 是 BYOA 专用请求函数。
// 与原 requestAt 的关键区别：不会把响应里的账号 Cookie 写回包级全局状态，避免 A 浏览器污染 B 浏览器。
func (d *QuarkUCShare) byoaRequestAt(ctx context.Context, api, credential, pathname, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	u := api + pathname
	req := base.RestyClient.R().SetContext(ctx)
	req.SetHeaders(map[string]string{
		"Cookie":     credential,
		"Accept":     "application/json, text/plain, */*",
		"User-Agent": d.conf.ua,
		"Referer":    d.conf.referer,
	})
	req.SetQueryParam("pr", d.conf.pr)
	req.SetQueryParam("entry", "ft")
	req.SetQueryParam("fr", "pc")
	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	var apiErr Resp
	req.SetError(&apiErr)

	res, err := req.Execute(method, u)
	if err != nil {
		return nil, err
	}
	if res.StatusCode() == http.StatusTooManyRequests {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
		res, err = req.Execute(method, u)
		if err != nil {
			return nil, err
		}
	}
	if quarkBYOAAuthExpired(res.StatusCode(), apiErr) {
		return nil, &byoa.AuthRequiredError{Provider: byoa.ProviderQuark}
	}
	if apiErr.Status >= 400 || apiErr.Code != 0 {
		if apiErr.Message == "" {
			return nil, errors.New("quark BYOA request failed")
		}
		return nil, errors.New(apiErr.Message)
	}
	return res.Body(), nil
}
