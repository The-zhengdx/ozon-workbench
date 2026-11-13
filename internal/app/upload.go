package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"ozon-workbench/internal/codexcli"
	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/media"
	"ozon-workbench/internal/store"
)

func (e *Engine) uploadMedia(ctx context.Context, id int64) (bool, error) {
	var moved bool
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(store.ForUpdate(ctx), id)
		if err != nil || task.Status != domain.StatusUploadingMedia {
			return err
		}
		pending, err := tx.PendingMessages(ctx, id)
		if err != nil {
			return err
		}
		now := e.now()
		if len(pending) > 0 {
			task.Status = domain.StatusRunning
			task.UpdatedAt = now
			moved = true
			return tx.UpdateTask(ctx, task)
		}
		reply, err := codexcli.ParseReply(task.LastReplyJSON)
		if err != nil {
			task.Status = domain.StatusFailed
			task.LastError = err.Error()
			task.FinishedAt = &now
			return tx.UpdateTask(ctx, task)
		}
		up := e.uploader()
		if !up.Configured() {
			task.Status = domain.StatusFailed
			task.LastError = "OSS 未配置，不能上传素材"
			task.FinishedAt = &now
			task.UpdatedAt = now
			return tx.UpdateTask(ctx, task)
		}
		keep := map[string]bool{}
		var eventMedia []map[string]string
		for _, file := range reply.Data.Media {
			keep[file.AssetID] = true
			full, err := media.Resolve(e.taskDir(task.ID), file.FilePath)
			if err != nil {
				task.Status = domain.StatusFailed
				task.LastError = err.Error()
				task.FinishedAt = &now
				return tx.UpdateTask(ctx, task)
			}
			ext := filepath.Ext(full)
			key := fmt.Sprintf("tasks/%d/%s/%s%s", task.ID, file.SKURef, file.AssetID, ext)
			existing := e.findAsset(ctx, tx, task.ID, file.AssetID)
			public := ""
			if existing.UploadStatus == domain.UploadUploaded && existing.ObjectKey == key && existing.PublicURL != "" {
				public = existing.PublicURL
			} else {
				obj, err := up.Upload(ctx, full, key)
				if err != nil {
					_, _ = tx.UpsertAsset(ctx, domain.Asset{
						TaskID: task.ID, AssetID: file.AssetID, SKURef: file.SKURef, Role: file.Role,
						LocalPath: full, ObjectKey: key, MimeType: file.MimeType, UploadStatus: domain.UploadFailed,
						ErrorMessage: err.Error(), CreatedAt: now, UpdatedAt: now,
					})
					task.Status = domain.StatusFailed
					task.LastError = "OSS 上传失败"
					task.FinishedAt = &now
					return tx.UpdateTask(ctx, task)
				}
				public = obj.PublicURL
			}
			if err := e.checkURL(ctx, public); err != nil {
				task.Status = domain.StatusFailed
				task.LastError = err.Error()
				task.FinishedAt = &now
				return tx.UpdateTask(ctx, task)
			}
			if _, err := tx.UpsertAsset(ctx, domain.Asset{
				TaskID: task.ID, AssetID: file.AssetID, SKURef: file.SKURef, Role: file.Role,
				LocalPath: full, ObjectKey: key, PublicURL: public, MimeType: file.MimeType,
				UploadStatus: domain.UploadUploaded, CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				return err
			}
			eventMedia = append(eventMedia, map[string]string{
				"asset_id": file.AssetID, "sku_ref": file.SKURef, "role": file.Role, "url": public,
			})
		}
		assets, err := tx.ListAssets(ctx, task.ID)
		if err != nil {
			return err
		}
		for _, asset := range assets {
			if !keep[asset.AssetID] && asset.UploadStatus == domain.UploadUploaded {
				asset.UploadStatus = domain.UploadSuperseded
				asset.UpdatedAt = now
				if _, err := tx.UpsertAsset(ctx, asset); err != nil {
					return err
				}
			}
		}
		payload, _ := json.Marshal(map[string]any{"event": "media_uploaded", "task_id": task.ID, "media": eventMedia})
		if _, err := tx.CreateMessage(ctx, domain.Message{
			TaskID: task.ID, Role: domain.RoleSystem, Kind: domain.KindMediaUploaded,
			Content: string(payload), Payload: payload, DeliveryStatus: domain.DeliveryPending, CreatedAt: now,
		}); err != nil {
			return err
		}
		task.Status = domain.StatusRunning
		task.UpdatedAt = now
		moved = true
		return tx.UpdateTask(ctx, task)
	})
	return moved, err
}

func (e *Engine) findAsset(ctx context.Context, tx store.Tx, taskID int64, assetID string) domain.Asset {
	assets, err := tx.ListAssets(ctx, taskID)
	if err != nil {
		return domain.Asset{}
	}
	for _, asset := range assets {
		if asset.AssetID == assetID {
			return asset
		}
	}
	return domain.Asset{}
}

func (e *Engine) checkURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("公开地址不是 HTTPS")
	}
	if e.VerifyURL != nil {
		return e.VerifyURL(ctx, raw)
	}
	return nil
}
