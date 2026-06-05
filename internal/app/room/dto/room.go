package dto

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

type CreateRoomInput struct {
	Title string
}

type CreateRoomRequest struct {
	Title string `json:"title" binding:"required,min=1,max=128"`
}

func (r CreateRoomRequest) Input() CreateRoomInput {
	return CreateRoomInput{Title: r.Title}
}

type RoomDetailDTO struct {
	ID            string                `json:"id"`
	MerchantID    string                `json:"merchant_id"`
	Title         string                `json:"title"`
	Status        model.RoomStatus      `json:"status"`
	CurrentItemID string                `json:"current_item_id"`
	OnlineCount   int                   `json:"online_count"`
	ItemQueue     []string              `json:"item_queue"`
	Item          []itemdto.ItemListDTO `json:"item"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

const (
	RoomFeedDefaultLimit = 10
	RoomFeedMaxLimit     = 50
)

type RoomFeedInput struct {
	Cursor string
	Limit  int
}

type RoomFeedCursor struct {
	CreatedAt time.Time
	ID        string
}

type roomFeedCursorPayload struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

type RoomFeedResult struct {
	List       []RoomDetailDTO `json:"list"`
	NextCursor string          `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

type MerchantRoomDTO struct {
	ID            string           `json:"id"`
	MerchantID    string           `json:"merchant_id"`
	Title         string           `json:"title"`
	Status        model.RoomStatus `json:"status"`
	StatusText    string           `json:"status_text"`
	CurrentItemID string           `json:"current_item_id"`
	OnlineCount   int              `json:"online_count"`
	QueuedCount   int              `json:"queued_count"`
	Actions       RoomActionsDTO   `json:"actions"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type RoomActionsDTO struct {
	CanStart bool `json:"can_start"`
	CanEnd   bool `json:"can_end"`
}

func NormalizeRoomFeedInput(input RoomFeedInput) RoomFeedInput {
	input.Cursor = strings.TrimSpace(input.Cursor)
	if input.Limit <= 0 {
		input.Limit = RoomFeedDefaultLimit
	}
	if input.Limit > RoomFeedMaxLimit {
		input.Limit = RoomFeedMaxLimit
	}
	return input
}

func EncodeRoomFeedCursor(cursor RoomFeedCursor) (string, error) {
	id := strings.TrimSpace(cursor.ID)
	if cursor.CreatedAt.IsZero() || id == "" {
		return "", errorx.ErrInvalidRequest
	}

	payload := roomFeedCursorPayload{
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:        id,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func DecodeRoomFeedCursor(value string) (*RoomFeedCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	body, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errorx.ErrInvalidRequest
	}

	var payload roomFeedCursorPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errorx.ErrInvalidRequest
	}

	id := strings.TrimSpace(payload.ID)
	if strings.TrimSpace(payload.CreatedAt) == "" || id == "" {
		return nil, errorx.ErrInvalidRequest
	}

	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil || createdAt.IsZero() {
		return nil, errorx.ErrInvalidRequest
	}

	return &RoomFeedCursor{CreatedAt: createdAt.UTC(), ID: id}, nil
}

func NewRoomDetailDTO(room *model.LiveRoom, onlineCount int, itemQueue []string, items []itemdto.ItemListDTO) RoomDetailDTO {
	if itemQueue == nil {
		itemQueue = []string{}
	}
	if items == nil {
		items = []itemdto.ItemListDTO{}
	}
	return RoomDetailDTO{
		ID:            room.ID,
		MerchantID:    room.MerchantID,
		Title:         room.Title,
		Status:        room.Status,
		CurrentItemID: room.CurrentItemID,
		OnlineCount:   onlineCount,
		ItemQueue:     itemQueue,
		Item:          items,
		CreatedAt:     room.CreatedAt,
		UpdatedAt:     room.UpdatedAt,
	}
}

func NewMerchantRoomDTO(room *model.LiveRoom, onlineCount int, queuedCount int) MerchantRoomDTO {
	return MerchantRoomDTO{
		ID:            room.ID,
		MerchantID:    room.MerchantID,
		Title:         room.Title,
		Status:        room.Status,
		StatusText:    roomStatusText(room.Status),
		CurrentItemID: room.CurrentItemID,
		OnlineCount:   onlineCount,
		QueuedCount:   queuedCount,
		Actions: RoomActionsDTO{
			CanStart: room.Status == model.RoomIdle,
			CanEnd:   room.Status == model.RoomLive,
		},
		CreatedAt: room.CreatedAt,
		UpdatedAt: room.UpdatedAt,
	}
}

func roomStatusText(status model.RoomStatus) string {
	switch status {
	case model.RoomIdle:
		return "未开播"
	case model.RoomLive:
		return "直播中"
	default:
		return string(status)
	}
}
