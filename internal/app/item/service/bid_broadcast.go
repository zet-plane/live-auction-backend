package service

import (
	"context"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type pendingBidBroadcast struct {
	roomID    string
	payload   dto.BidSuccessPayload
	timer     *time.Timer
	createdAt time.Time
	bids      int64
}

func (s *Service) enqueueBidSuccess(roomID string, payload dto.BidSuccessPayload) {
	if s.broadcaster == nil {
		return
	}
	delay := s.bidBroadcastDelay
	if delay <= 0 {
		err := s.broadcaster.Fanout(wsevent.RoomTopic(roomID), wsevent.Event{
			Type:    dto.EventBidSuccess,
			Payload: payload,
		})
		recordBidBroadcastMetric("flush", fanoutResult(err), 1, 0, 0)
		return
	}

	key := roomID + "/" + payload.ItemID
	var metric observability.BidBroadcastMetric
	shouldRecord := false
	s.bidBroadcastMu.Lock()
	if s.pendingBidBroadcasts == nil {
		s.pendingBidBroadcasts = make(map[string]*pendingBidBroadcast)
	}
	if pending := s.pendingBidBroadcasts[key]; pending != nil {
		pending.payload = payload
		pending.bids++
		metric = bidBroadcastMetric("enqueue_update", "success", pending.bids, int64(len(s.pendingBidBroadcasts)), 0)
		shouldRecord = true
		s.bidBroadcastMu.Unlock()
		recordBidBroadcastMetricValue(metric, shouldRecord)
		return
	}
	pending := &pendingBidBroadcast{roomID: roomID, payload: payload, createdAt: time.Now(), bids: 1}
	s.pendingBidBroadcasts[key] = pending
	metric = bidBroadcastMetric("enqueue_create", "success", pending.bids, int64(len(s.pendingBidBroadcasts)), 0)
	shouldRecord = true
	pending.timer = time.AfterFunc(delay, func() {
		s.flushBidSuccess(key)
	})
	s.bidBroadcastMu.Unlock()
	recordBidBroadcastMetricValue(metric, shouldRecord)
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
	pendingCount := int64(len(s.pendingBidBroadcasts))
	s.bidBroadcastMu.Unlock()
	s.fanoutBidSuccess(pending, pendingCount)
}

func (s *Service) flushBidSuccess(key string) {
	s.bidBroadcastMu.Lock()
	pending := s.pendingBidBroadcasts[key]
	if pending == nil {
		s.bidBroadcastMu.Unlock()
		return
	}
	delete(s.pendingBidBroadcasts, key)
	pendingCount := int64(len(s.pendingBidBroadcasts))
	s.bidBroadcastMu.Unlock()
	s.fanoutBidSuccess(pending, pendingCount)
}

func (s *Service) fanoutBidSuccess(pending *pendingBidBroadcast, pendingCount int64) {
	if pending == nil || s.broadcaster == nil {
		return
	}
	err := s.broadcaster.Fanout(wsevent.RoomTopic(pending.roomID), wsevent.Event{
		Type:    dto.EventBidSuccess,
		Payload: pending.payload,
	})
	recordBidBroadcastMetric("flush", fanoutResult(err), pending.bids, pendingCount, time.Since(pending.createdAt))
}

func bidBroadcastMetric(action, result string, bids, pending int64, duration time.Duration) observability.BidBroadcastMetric {
	return observability.BidBroadcastMetric{
		Action:    action,
		Result:    result,
		EventType: dto.EventBidSuccess,
		Bids:      bids,
		Pending:   pending,
		Duration:  duration,
	}
}

func recordBidBroadcastMetric(action, result string, bids, pending int64, duration time.Duration) {
	recordBidBroadcastMetricValue(bidBroadcastMetric(action, result, bids, pending, duration), true)
}

func recordBidBroadcastMetricValue(metric observability.BidBroadcastMetric, ok bool) {
	if !ok {
		return
	}
	observability.DefaultRecorder().BidBroadcast(context.Background(), metric)
}

func fanoutResult(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}
