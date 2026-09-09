package aliyundrive_share2_open

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/byoa"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

var (
	aliyunBYOAShareDownloadEndpoint = "https://api.alipan.com/v2/file/get_share_link_download_url"
	aliyunBYOASharePreviewEndpoint  = "https://api.alipan.com/v2/file/get_share_link_video_preview_play_info"
)

// byoaDirectLink 使用当前浏览器自己的阿里普通 Access Token，直接从分享接口获取播放地址。
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

	requestLink := func() (string, *ErrorResp, error) {
		return requestAliyunBYOAShareURL(base.GetAliyunRestyClient(), ctx, accessToken, d.ShareToken, driveID, file.GetID(), d.ShareId)
	}

	url, apiErr, err := requestLink()
	if err != nil {
		return nil, err
	}

	if apiErr != nil && apiErr.Code == "ShareLinkTokenInvalid" {
		if err := d.getShareToken(); err != nil {
			return nil, err
		}
		url, apiErr, err = requestLink()
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

	if url == "" {
		return nil, errors.New("aliyun share playback URL unavailable")
	}

	log.Infof("[BYOA][Aliyun] 获取分享播放链接 %v %v", file.GetName(), file.GetSize())
	return &model.Link{
		URL: url,
		Header: http.Header{
			"Referer":    []string{"https://www.alipan.com/"},
			"User-Agent": []string{conf.UserAgent},
		},
	}, nil
}

func requestAliyunBYOAShareURL(client *resty.Client, ctx context.Context, accessToken, shareToken, driveID, fileID, shareID string) (string, *ErrorResp, error) {
	data := base.Json{
		"drive_id":   driveID,
		"file_id":    fileID,
		"expire_sec": 600,
		"share_id":   shareID,
	}
	var download ShareLinkResp
	var apiErr ErrorResp
	httpResp, err := client.R().
		SetContext(ctx).
		SetError(&apiErr).
		SetHeader("content-type", "application/json").
		SetHeader("Authorization", "Bearer\t"+accessToken).
		SetHeader(CanaryHeaderKey, CanaryHeaderValue).
		SetHeader("x-share-token", shareToken).
		SetBody(data).
		SetResult(&download).
		Post(aliyunBYOAShareDownloadEndpoint)
	if err != nil {
		return "", nil, err
	}
	if httpResp.StatusCode() != http.StatusGone {
		if httpResp.IsError() && apiErr.Code == "" {
			return "", nil, fmt.Errorf("aliyun share download http status: %d", httpResp.StatusCode())
		}
		if download.DownloadUrl != "" {
			return download.DownloadUrl, &apiErr, nil
		}
		return download.Url, &apiErr, nil
	}

	// 阿里已停用原分享下载接口（HTTP 410）。视频仍可通过无需转存的分享预览接口播放。
	data["category"] = "live_transcoding"
	data["template_id"] = ""
	data["get_preview_url"] = true
	apiErr = ErrorResp{}
	var preview VideoPreviewResponse
	httpResp, err = client.R().
		SetContext(ctx).
		SetError(&apiErr).
		SetHeader("content-type", "application/json").
		SetHeader("Authorization", "Bearer\t"+accessToken).
		SetHeader(CanaryHeaderKey, CanaryHeaderValue).
		SetHeader("x-share-token", shareToken).
		SetBody(data).
		SetResult(&preview).
		Post(aliyunBYOASharePreviewEndpoint)
	if err != nil {
		return "", nil, err
	}
	if httpResp.IsError() {
		if apiErr.Code != "" {
			return "", &apiErr, nil
		}
		return "", nil, fmt.Errorf("aliyun share preview http status: %d", httpResp.StatusCode())
	}
	for _, video := range preview.PlayInfo.Videos {
		if video.Url != "" {
			return video.Url, &apiErr, nil
		}
		if video.PreviewUrl != "" {
			return video.PreviewUrl, &apiErr, nil
		}
	}
	return "", &apiErr, nil
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
