package bootstrap

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	log "github.com/sirupsen/logrus"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

func LoadStorages() {
	storages, err := db.GetEnabledStorages()
	if err != nil {
		utils.Log.Fatalf("failed get enabled storages: %+v", err)
	}

	log.Infof("total %v enabled storages", len(storages))
	conf.LazyLoad = setting.GetBool("ali_lazy_load")

	go func(storages []model.Storage) {
		for i := range storages {
			storage := storages[i]
			err := op.LoadStorage(context.Background(), storage)
			if err != nil {
				log.Errorf("[%d] failed get enabled storages [%s], %+v",
					i+1, storage.MountPath, err)
			} else {
				log.Infof("[%d] success load storage: [%s], driver: [%s]",
					i+1, storage.MountPath, storage.Driver)
			}
		}
		conf.SendStoragesLoadedSignal()

		log.Infof("=== load storages completed ===")
		if conf.LazyLoad {
			syncStatus(2)
			go validateStorages()
		} else {
			syncStatus(3)
		}
	}(storages)
}

func validateStorages() {
	op.ValidateStorages()
	syncStatus(3)
}

func byoaRuntimeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BYOA_XIAOYA_BOOTSTRAP"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func syncStatus(code int) {
	// BYOA 独立运行时不包含 alist-tvbox 的 4567 控制服务。
	// 仅跳过遗留状态回调；存储加载和 validateStorages 仍照常执行。
	if byoaRuntimeEnabled() {
		return
	}

	url := "http://127.0.0.1:4567/api/alist/status?code=" + strconv.Itoa(code)
	_, err := base.RestyClient.R().
		SetHeader("X-API-KEY", setting.GetStr("atv_api_key")).
		Post(url)
	if err != nil {
		log.Warnf("sync status failed: %v", err)
	}
}
