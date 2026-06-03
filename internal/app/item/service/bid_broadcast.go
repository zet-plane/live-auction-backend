package service

import (
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type pendingBidBroadcast struct {
	roomID  string
	payload dto.BidSuccessPayload
	timer   *time.Timer
}

func (s *Service) enqueueBidSuccess(roomID string, payload dto.BidSuccessPayload) {
	if s.broadcaster == nil {
		return
	}
	delay := s.bidBroadcastDelay
	if delay <= 0 {
		_ = s.broadcaster.Fanout(wsevent.RoomTopic(roomID), wsevent.Event{
			Type:    dto.EventBidSuccess,
			Payload: payload,
		})
		return
	}

	key := roomID + "/" + payload.ItemID
	s.bidBroadcastMu.Lock()
	if s.pendingBidBroadcasts == nil {
		s.pendingBidBroadcasts = make(map[string]*pendingBidBroadcast)
	}
	if pending := s.pendingBidBroadcasts[key]; pending != nil {
		pending.payload = payload
		s.bidBroadcastMu.Unlock()
		return
	}
	pending := &pendingBidBroadcast{roomID: roomID, payload: payload}
	s.pendingBidBroadcasts[key] = pending
	pending.timer = time.AfterFunc(delay, func() {
		s.flushBidSuccess(key)
	})
	s.bidBroadcastMu.Unlock()
}

func (s *Service) flushBidSuccessNow(roomID, itemID string) {
	key := roomID + "/" + itemID
	s.bidBroadcastMu.Lock()
	pending := s.pendingBidBroadcasts[key]
	if pending == nil {
		s.bidBroadcastMu.Unlock()
		return
	}
	delete(s.pendingBidBroadcasts, key)
	if pending.timer != nil {
		pending.timer.Stop()
	}
	s.bidBroadcastMu.Unlock()
	s.fanoutBidSuccess(pending)
}

func (s *Service) flushBidSuccess(key string) {
	s.bidBroadcastMu.Lock()
	pending := s.pendingBidBroadcasts[key]
	if pending == nil {
		s.bidBroadcastMu.Unlock()
		return
	}
	delete(s.pendingBidBroadcasts, key)
	s.bidBroadcastMu.Unlock()
	s.fanoutBidSuccess(pending)
}

func (s *Service) fanoutBidSuccess(pending *pendingBidBroadcast) {
	if pending == nil || s.broadcaster == nil {
		return
	}
	_ = s.broadcaster.Fanout(wsevent.RoomTopic(pending.roomID), wsevent.Event{
		Type:    dto.EventBidSuccess,
		Payload: pending.payload,
	})
}
