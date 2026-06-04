package dto

import (
	"time"

	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
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
