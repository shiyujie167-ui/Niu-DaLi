package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	VideoWorkspaceMaxAssetBytes        = 10 << 20
	VideoWorkspaceAssetQuota           = 100 << 20
	VideoWorkspaceSubmissionSubmitting = "submitting"
	VideoWorkspaceSubmissionAccepted   = "accepted"
	VideoWorkspaceSubmissionFailed     = "failed"
	VideoWorkspaceSubmissionUnknown    = "unknown"
)

var (
	ErrVideoCanvasConflict          = errors.New("canvas revision has changed")
	ErrVideoCanvasAssetQuota        = errors.New("canvas image storage limit reached")
	ErrVideoCanvasSubmissionInvalid = errors.New("invalid canvas generation node or submission")
)

// Native binary types avoid JSON dialect differences and local disk affinity.
type VideoWorkspaceCanvas struct {
	UserID     int    `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Revision   int64  `json:"revision" gorm:"not null"`
	Graph      []byte `json:"-" gorm:"size:1048576;not null"`
	AssetBytes int64  `json:"-" gorm:"not null"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type VideoWorkspaceAsset struct {
	ID          string `json:"id" gorm:"type:varchar(36);primaryKey"`
	UserID      int    `json:"-" gorm:"not null;uniqueIndex:idx_video_asset_owner_hash"`
	ContentHash string `json:"-" gorm:"type:char(64);not null;uniqueIndex:idx_video_asset_owner_hash"`
	Filename    string `json:"filename" gorm:"type:varchar(255);not null"`
	MimeType    string `json:"mime_type" gorm:"type:varchar(32);not null"`
	Size        int64  `json:"size" gorm:"not null"`
	Width       int    `json:"width" gorm:"not null"`
	Height      int    `json:"height" gorm:"not null"`
	Data        []byte `json:"-" gorm:"size:10485760;not null"`
	CreatedAt   int64  `json:"created_at"`
}

type VideoWorkspaceSubmission struct {
	ID           int64  `json:"-" gorm:"primaryKey"`
	UserID       int    `json:"-" gorm:"not null;uniqueIndex:idx_video_submission_owner_id;index:idx_video_submission_owner_node"`
	SubmissionID string `json:"submission_id" gorm:"type:varchar(96);not null;uniqueIndex:idx_video_submission_owner_id"`
	NodeID       string `json:"node_id" gorm:"type:varchar(96);not null;index:idx_video_submission_owner_node"`
	TaskID       string `json:"task_id,omitempty" gorm:"type:varchar(191)"`
	Status       string `json:"status" gorm:"type:varchar(16);not null"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

func NewVideoWorkspaceCanvas(userID int) *VideoWorkspaceCanvas {
	return &VideoWorkspaceCanvas{UserID: userID, Graph: []byte(`{"schema_version":1,"nodes":[],"edges":[],"viewport":{"x":0,"y":0,"zoom":1}}`)}
}

func GetVideoWorkspaceCanvas(userID int) (*VideoWorkspaceCanvas, error) {
	canvas := NewVideoWorkspaceCanvas(userID)
	err := DB.Where("user_id = ?", userID).First(canvas).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewVideoWorkspaceCanvas(userID), nil
	}
	return canvas, err
}

// Compare-and-swap prevents a stale browser tab from overwriting a newer graph.
func SaveVideoWorkspaceCanvas(userID int, revision int64, graph []byte) (*VideoWorkspaceCanvas, error) {
	var saved *VideoWorkspaceCanvas
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(NewVideoWorkspaceCanvas(userID)).Error; err != nil {
			return err
		}
		result := tx.Model(&VideoWorkspaceCanvas{}).Where("user_id = ? AND revision = ?", userID, revision).
			Updates(map[string]any{"graph": graph, "revision": revision + 1, "updated_at": time.Now().Unix()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVideoCanvasConflict
		}
		saved = &VideoWorkspaceCanvas{UserID: userID, Revision: revision + 1, Graph: graph}
		return nil
	})
	return saved, err
}

func (asset *VideoWorkspaceAsset) Bytes() ([]byte, error) {
	if len(asset.Data) == 0 || int64(len(asset.Data)) != asset.Size || asset.Size > VideoWorkspaceMaxAssetBytes {
		return nil, errors.New("invalid stored canvas image")
	}
	return append([]byte(nil), asset.Data...), nil
}

func GetVideoWorkspaceAsset(userID int, id string) (*VideoWorkspaceAsset, error) {
	var asset VideoWorkspaceAsset
	err := DB.Where("user_id = ? AND id = ?", userID, id).First(&asset).Error
	return &asset, err
}

func CreateVideoWorkspaceAsset(asset *VideoWorkspaceAsset) (*VideoWorkspaceAsset, error) {
	asset.ID = uuid.NewString()
	asset.ContentHash = fmt.Sprintf("%x", sha256.Sum256(asset.Data))
	asset.Size = int64(len(asset.Data))
	if asset.UserID <= 0 || asset.Size <= 0 || asset.Size > VideoWorkspaceMaxAssetBytes {
		return nil, errors.New("invalid canvas image")
	}
	var saved VideoWorkspaceAsset
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(NewVideoWorkspaceCanvas(asset.UserID)).Error; err != nil {
			return err
		}
		// Owner/hash uniqueness also makes repeated identical uploads idempotent.
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(asset).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND content_hash = ?", asset.UserID, asset.ContentHash).First(&saved).Error; err != nil {
			return err
		}
		if saved.ID != asset.ID {
			return nil
		}
		result := tx.Model(&VideoWorkspaceCanvas{}).Where("user_id = ? AND asset_bytes <= ?", asset.UserID, VideoWorkspaceAssetQuota-asset.Size).
			Update("asset_bytes", gorm.Expr("asset_bytes + ?", asset.Size))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVideoCanvasAssetQuota
		}
		return nil
	})
	return &saved, err
}

// A claim is never released: an ambiguous upstream response must not spend twice.
func ClaimVideoWorkspaceSubmission(userID int, nodeID, submissionID string) (*VideoWorkspaceSubmission, bool, error) {
	if userID <= 0 || nodeID == "" || len(nodeID) > 96 || submissionID == "" || len(submissionID) > 96 {
		return nil, false, ErrVideoCanvasSubmissionInvalid
	}
	var run VideoWorkspaceSubmission
	created := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		// SQLite has no FOR UPDATE. This harmless write acquires its writer
		// lock before reading receipts; all engines serialize on the canvas row.
		if tx.Dialector.Name() == "sqlite" {
			if err := tx.Model(&VideoWorkspaceCanvas{}).Where("user_id = ?", userID).UpdateColumn("asset_bytes", gorm.Expr("asset_bytes")).Error; err != nil {
				return err
			}
		}
		var canvas VideoWorkspaceCanvas
		if err := lockForUpdate(tx).Where("user_id = ?", userID).First(&canvas).Error; err != nil {
			return err
		}
		err := tx.Where("user_id = ? AND submission_id = ?", userID, submissionID).First(&run).Error
		if err == nil {
			if run.NodeID != nodeID {
				return ErrVideoCanvasSubmissionInvalid
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var graph struct {
			Nodes []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"nodes"`
		}
		if err := common.Unmarshal(canvas.Graph, &graph); err != nil {
			return err
		}
		found := false
		for _, node := range graph.Nodes {
			if node.ID == nodeID && node.Type == "generation" {
				found = true
				break
			}
		}
		if !found {
			return ErrVideoCanvasSubmissionInvalid
		}
		var previous VideoWorkspaceSubmission
		err = tx.Where("user_id = ? AND node_id = ? AND status IN ?", userID, nodeID, []string{VideoWorkspaceSubmissionSubmitting, VideoWorkspaceSubmissionUnknown, VideoWorkspaceSubmissionAccepted}).Order("id DESC").First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			pending := previous.Status != VideoWorkspaceSubmissionAccepted
			if !pending {
				var task Task
				if err := tx.Select("status").Where("user_id = ? AND task_id = ?", userID, previous.TaskID).First(&task).Error; err != nil {
					// Missing task state cannot prove that it is safe to submit again.
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						return err
					}
					pending = true
				} else {
					pending = task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure
				}
			}
			if pending {
				run = previous
				return nil
			}
		}
		run = VideoWorkspaceSubmission{UserID: userID, NodeID: nodeID, SubmissionID: submissionID, Status: VideoWorkspaceSubmissionSubmitting}
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return &run, created, err
}

func CompleteVideoWorkspaceSubmission(userID int, submissionID, taskID string) error {
	return CompleteVideoWorkspaceSubmissionWithTx(DB, userID, submissionID, taskID)
}

func CompleteVideoWorkspaceSubmissionWithTx(tx *gorm.DB, userID int, submissionID, taskID string) error {
	if submissionID == "" || taskID == "" {
		return ErrVideoCanvasSubmissionInvalid
	}
	result := tx.Model(&VideoWorkspaceSubmission{}).
		Where("user_id = ? AND submission_id = ? AND (task_id = ? OR task_id = ?)", userID, submissionID, "", taskID).
		Updates(map[string]any{"task_id": taskID, "status": VideoWorkspaceSubmissionAccepted, "updated_at": time.Now().Unix()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := tx.Model(&VideoWorkspaceSubmission{}).Where("user_id = ? AND submission_id = ? AND task_id = ? AND status = ?", userID, submissionID, taskID, VideoWorkspaceSubmissionAccepted).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrVideoCanvasSubmissionInvalid
		}
	}
	return nil
}

func FailVideoWorkspaceSubmission(userID int, submissionID string, ambiguous bool) error {
	status := VideoWorkspaceSubmissionFailed
	if ambiguous {
		status = VideoWorkspaceSubmissionUnknown
	}
	return DB.Model(&VideoWorkspaceSubmission{}).
		Where("user_id = ? AND submission_id = ? AND status = ?", userID, submissionID, VideoWorkspaceSubmissionSubmitting).
		Updates(map[string]any{"status": status, "updated_at": time.Now().Unix()}).Error
}
