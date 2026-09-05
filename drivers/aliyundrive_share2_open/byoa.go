package aliyundrive_share2_open

import (
	"context"
	"errors"
	"net/http"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	log "github.com/sirupsen/logrus"
)

// byoaDirectLink 使用当前浏览器自己的阿里普通 Access Token，直接从分享接口获取下载地址。
// 该路径不转存到个人盘、不依赖 AliyundriveOpen、不使用服务器账号池和账号相关 Link Cache。
// MVP 中 Access Token 过期后直接要求用户重新扫码，不做服务端 Refresh Token 生命周期管理。
func (d *AliyundriveShare2Open) byoaDirectLink(ctx context.Context, file model.Obj, accessToken string) (*model.Link, error) {
	if d.ShareToken == "" {
		if err := d.getShareToken(); err != nil {
			return nil, err
		}
	}

	driveID, err := d.byoaShareDriveID()
	if err != nil {
		return nil, err
	}

	requestLink := func() (*ShareLinkResp, *ErrorResp, error) {
		data := base.Json{
			"drive_id":   driveID,
			"file_id":    file.GetID(),
			"expire_sec": 600,
			"share_id":   d.ShareId,
		}
		var resp ShareLinkResp
		var apiErr ErrorResp
		req := base.RestyClient.R().
			SetContext(ctx).
			SetError(&apiErr).
			SetHeader("content-type", "application/json").
			SetHeader("Authorization", "Bearer\t"+accessToken).
			SetHeader(CanaryHeaderKey, CanaryHeaderValue).
			SetHeader("x-share-token", d.ShareToken).
			SetBody(data).
			SetResult(&resp)
		_, reqErr := req.Post("https://api.alipan.com/v2/file/get_share_link_download_url")
		return &resp, &apiErr, reqErr
	}

	resp, apiErr, err := requestLink()
	if err != nil {
		return nil, err
	}

	if apiErr != nil && apiErr.Code == "ShareLinkTokenInvalid" {
		if err := d.getShareToken(); err != nil {
			return nil, err
		}
		resp, apiErr, err = requestLink()
		if err != nil {
			return nil, err
		}
	}

	if apiErr != nil && apiErr.Code != "" {
		if apiErr.Code == "AccessTokenInvalid" || apiErr.Code == "AccessTokenExpired" {
			return nil, &byoa.AuthRequiredError{Provider: byoa.ProviderAliyun}
		}
		if apiErr.Message != "" {
			return nil, errors.New(apiErr.Code + ": " + apiErr.Message)
		}
		return nil, errors.New(apiErr.Code)
	}

	if resp == nil || resp.DownloadUrl == "" {
		return nil, errors.New("empty aliyun share download url")
	}

	log.Infof("[BYOA][Aliyun] 获取分享直链 %v %v", file.GetName(), file.GetSize())
	return &model.Link{
		URL: resp.DownloadUrl,
		Header: http.Header{
			"Referer":    []string{"https://www.alipan.com/"},
			"User-Agent": []string{conf.UserAgent},
		},
	}, nil
}

// byoaShareDriveID 从公开分享列表中取得分享所属 drive_id。
// Xiaoya 的 AliyundriveShare2Open 列目录本身不依赖访客个人账号，因此无需保存服务器私人 Token。
func (d *AliyundriveShare2Open) byoaShareDriveID() (string, error) {
	rootID := d.RootFolderID
	if rootID == "" {
		rootID = "root"
	}
	files, err := d.getFiles(rootID)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if file.DriveId != "" {
			return file.DriveId, nil
		}
	}
	return "", errors.New("aliyun share drive id not found")
}
