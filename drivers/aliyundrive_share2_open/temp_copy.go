package aliyundrive_share2_open

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

const (
	aliyunBYOATempFolderName = ".Xiaoya-BYOA-Temp"
)

var (
	aliyunDriveInfoEndpoint   = "https://api.alipan.com/v2/user/get"
	aliyunFileListEndpoint    = "https://api.alipan.com/v2/file/list"
	aliyunFileCreateEndpoint  = "https://api.alipan.com/adrive/v2/file/createWithFolders"
	aliyunFileCopyEndpoint    = "https://api.alipan.com/adrive/v4/batch"
	aliyunFilePreviewEndpoint = "https://api.alipan.com/v2/file/get_video_preview_play_info"
	aliyunFileDeleteEndpoint  = "https://api.alipan.com/v2/recyclebin/trash"
)

type aliyunTempCopy struct {
	driveID  string
	parentID string
	fileID   string
}

func (c aliyunTempCopy) valid() bool {
	return c.driveID != "" && c.parentID != "" && c.fileID != "" && c.parentID != "root"
}

func (d *AliyundriveShare2Open) byoaTempCopyLink(ctx context.Context, file model.Obj, accessToken string) (*model.Link, error) {
	client := resty.New()
	driveID, err := aliyunBYOAUserDriveID(ctx, client, accessToken)
	if err != nil {
		return nil, err
	}
	parentID, err := aliyunBYOATempFolder(ctx, client, accessToken, driveID)
	if err != nil {
		return nil, err
	}
	copied := aliyunTempCopy{driveID: driveID, parentID: parentID}
	copied.fileID, err = d.byoaCopyShareFile(ctx, client, accessToken, driveID, parentID, file.GetID())
	if err != nil {
		return nil, err
	}
	if aliyunBYOACleanupEnabled() {
		defer func() {
			if err := aliyunBYOACleanup(ctx, client, accessToken, copied); err != nil {
				log.Warn("[BYOA][Aliyun] temporary cleanup cleanup_success=false")
			} else {
				log.Info("[BYOA][Aliyun] temporary cleanup cleanup_success=true")
			}
		}()
	}
	url, err := aliyunBYOAVideoURL(ctx, client, accessToken, copied)
	if err != nil {
		return nil, err
	}
	log.Infof("[BYOA][Aliyun] temporary playback copy_created=true url_present=%t", url != "")
	return &model.Link{URL: url, Header: http.Header{
		"Referer":    []string{"https://www.alipan.com/"},
		"User-Agent": []string{conf.UserAgent},
	}}, nil
}

func aliyunBYOACleanupEnabled() bool {
	return strings.ToLower(strings.TrimSpace(os.Getenv("BYOA_ALIYUN_TEMP_CLEANUP"))) != "off"
}

func aliyunBYOARequest(ctx context.Context, client *resty.Client, token, shareToken, endpoint string, body interface{}, result interface{}) error {
	req := client.R().SetContext(ctx).SetHeader("content-type", "application/json").
		SetHeader("Authorization", "Bearer\t"+token).SetBody(body).SetResult(result)
	if shareToken != "" {
		req.SetHeader("x-share-token", shareToken)
	}
	resp, err := req.Post(endpoint)
	if err != nil {
		return err
	}
	if resp.StatusCode() == http.StatusTooManyRequests {
		return errors.New("阿里云请求过于频繁，请稍后重试")
	}
	if resp.IsError() {
		return fmt.Errorf("aliyun temporary operation http status: %d", resp.StatusCode())
	}
	return nil
}

func aliyunBYOAUserDriveID(ctx context.Context, client *resty.Client, token string) (string, error) {
	var result struct {
		DefaultDriveID string `json:"default_drive_id"`
	}
	if err := aliyunBYOARequest(ctx, client, token, "", aliyunDriveInfoEndpoint, map[string]string{}, &result); err != nil {
		return "", err
	}
	if result.DefaultDriveID == "" {
		return "", errors.New("aliyun default drive unavailable")
	}
	return result.DefaultDriveID, nil
}

func aliyunBYOATempFolder(ctx context.Context, client *resty.Client, token, driveID string) (string, error) {
	var created struct {
		FileID string `json:"file_id"`
	}
	err := aliyunBYOARequest(ctx, client, token, "", aliyunFileCreateEndpoint, map[string]interface{}{
		"drive_id": driveID, "parent_file_id": "root", "name": aliyunBYOATempFolderName,
		"type": "folder", "check_name_mode": "refuse",
	}, &created)
	if err == nil && created.FileID != "" {
		return created.FileID, nil
	}
	var listed struct {
		Items []struct {
			FileID string `json:"file_id"`
			Name   string `json:"name"`
			Type   string `json:"type"`
		} `json:"items"`
	}
	if listErr := aliyunBYOARequest(ctx, client, token, "", aliyunFileListEndpoint, map[string]interface{}{
		"drive_id": driveID, "parent_file_id": "root", "limit": 100,
	}, &listed); listErr != nil {
		if err != nil {
			return "", err
		}
		return "", listErr
	}
	for _, item := range listed.Items {
		if item.Name == aliyunBYOATempFolderName && item.Type == "folder" && item.FileID != "" {
			return item.FileID, nil
		}
	}
	return "", errors.New("aliyun BYOA temporary folder unavailable")
}

func (d *AliyundriveShare2Open) byoaCopyShareFile(ctx context.Context, client *resty.Client, token, driveID, parentID, sourceID string) (string, error) {
	var result struct {
		Responses []struct {
			Body struct {
				FileID string `json:"file_id"`
			} `json:"body"`
		} `json:"responses"`
	}
	err := aliyunBYOARequest(ctx, client, token, d.ShareToken, aliyunFileCopyEndpoint, map[string]interface{}{
		"requests": []map[string]interface{}{{"body": map[string]interface{}{
			"file_id": sourceID, "share_id": d.ShareId, "auto_rename": true,
			"to_parent_file_id": parentID, "to_drive_id": driveID,
		}, "headers": map[string]string{"Content-Type": "application/json"}, "id": "0", "method": "POST", "url": "/file/copy"}},
		"resource": "file",
	}, &result)
	if err != nil {
		return "", err
	}
	if len(result.Responses) == 0 || result.Responses[0].Body.FileID == "" {
		return "", errors.New("aliyun temporary copy did not return a new file_id")
	}
	return result.Responses[0].Body.FileID, nil
}

func aliyunBYOAVideoURL(ctx context.Context, client *resty.Client, token string, copied aliyunTempCopy) (string, error) {
	if !copied.valid() {
		return "", errors.New("invalid BYOA temporary copy record")
	}
	var result VideoPreviewResponse
	err := aliyunBYOARequest(ctx, client, token, "", aliyunFilePreviewEndpoint, map[string]interface{}{
		"drive_id": copied.driveID, "file_id": copied.fileID, "category": "live_transcoding", "url_expire_sec": 14400,
	}, &result)
	if err != nil {
		return "", err
	}
	videos := append([]LiveTranscoding(nil), result.PlayInfo.Videos...)
	sort.SliceStable(videos, func(i, j int) bool { return videoRank(videos[i].TemplateId) < videoRank(videos[j].TemplateId) })
	for _, video := range videos {
		if strings.EqualFold(video.Status, "finished") {
			if video.Url != "" {
				return video.Url, nil
			}
			if video.PreviewUrl != "" {
				return video.PreviewUrl, nil
			}
		}
	}
	return "", errors.New("aliyun finished playback URL unavailable")
}

func videoRank(template string) int {
	switch strings.ToUpper(template) {
	case "FHD":
		return 0
	case "HD":
		return 1
	case "SD":
		return 2
	default:
		return 3
	}
}

func aliyunBYOACleanup(ctx context.Context, client *resty.Client, token string, copied aliyunTempCopy) error {
	if !copied.valid() {
		return errors.New("refuse cleanup without exact newly-created file_id")
	}
	return aliyunBYOARequest(ctx, client, token, "", aliyunFileDeleteEndpoint, map[string]string{
		"drive_id": copied.driveID, "file_id": copied.fileID,
	}, &struct{}{})
}
