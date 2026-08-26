package handles

import (
	stdpath "path"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ShareSaveReq 服务端转存请求:把分享挂载里的文件/目录转存到同族账号存储。
type ShareSaveReq struct {
	// SrcDir 源目录(分享挂载下的绝对路径)
	SrcDir string `json:"src_dir" binding:"required"`
	// Names 源目录下的对象名,支持文件与目录(目录整棵递归转存)
	Names []string `json:"names" binding:"required,min=1,max=100"`
	// DstDir 目标目录(网盘账号挂载下的绝对路径,须已存在)
	DstDir string `json:"dst_dir" binding:"required"`
}

// FsShareSave 分享服务端转存:源挂载驱动实现了 driver.ShareSaver 才可用,
// 目标必须是同族网盘账号存储;同步完成(网盘侧秒转),返回新建对象 fid。
func FsShareSave(c *gin.Context) {
	user := c.Request.Context().Value(conf.UserKey).(*model.User)

	var req ShareSaveReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	srcPath, err := user.JoinPath(req.SrcDir)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}
	dstPath, err := user.JoinPath(req.DstDir)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 权限:目标侧要求可写(源是分享挂载,读取无需写权限)
	meta, err := op.GetNearestMeta(dstPath)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500, true)
		return
	}
	if !common.CanWrite(user, meta, dstPath) {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	srcStorage, srcActualPath, err := op.GetStorageAndActualPath(srcPath)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	saver, ok := srcStorage.(driver.ShareSaver)
	if !ok {
		common.ErrorResp(c, errors.New("源存储不支持服务端转存"), 400)
		return
	}

	objs := make([]model.Obj, 0, len(req.Names))
	for _, name := range req.Names {
		obj, err := op.Get(c.Request.Context(), srcStorage, stdpath.Join(srcActualPath, name))
		if err != nil {
			common.ErrorResp(c, errors.Wrapf(err, "获取源对象 %s 失败", name), 500)
			return
		}
		objs = append(objs, obj)
	}

	dstStorage, dstActualPath, err := op.GetStorageAndActualPath(dstPath)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	dstDir, err := op.Get(c.Request.Context(), dstStorage, dstActualPath)
	if err != nil {
		common.ErrorResp(c, errors.Wrap(err, "获取目标目录失败"), 500)
		return
	}
	if !dstDir.IsDir() {
		common.ErrorResp(c, errs.NotFolder, 400)
		return
	}

	fids, err := saver.SaveTo(c.Request.Context(), dstStorage, dstDir, objs)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	common.SuccessResp(c, gin.H{"saved": len(fids), "fids": fids})
}
