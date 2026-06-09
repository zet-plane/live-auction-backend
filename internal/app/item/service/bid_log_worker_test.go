package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
)

type fakeBidLogStreamReader struct {
	messages         []itemcache.BidLogStreamMessage
	pendingMessages  []itemcache.BidLogStreamMessage
	readErr          error
	readPendingErr   error
	ackErr           error
	acks             []string
	readCalls        int
	readPendingCalls int
}

func (r *fakeBidLogStreamReader) Read(_ context.Context, _ int) ([]itemcache.BidLogStreamMessage, error) {
	r.readCalls++
	if r.readErr != nil {
		return nil, r.readErr
	}
	return r.messages, nil
}

func (r *fakeBidLogStreamReader) ReadPending(_ context.Context, _ int) ([]itemcache.BidLogStreamMessage, error) {
	r.readPendingCalls++
	if r.readPendingErr != nil {
		return nil, r.readPendingErr
	}
	return r.pendingMessages, nil
}

func (r *fakeBidLogStreamReader) Ack(_ context.Context, ids []string) error {
	r.acks = append(r.acks, ids...)
	if r.ackErr != nil {
		return r.ackErr
	}
	return nil
}

type fakeBidLogBatchStore struct {
	logs        []*itemmodel.BidLog
	err         error
	duplicateOK bool
	calls       int
}

func (s *fakeBidLogBatchStore) CreateBidLogs(logs []*itemmodel.BidLog) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	if s.duplicateOK {
		return nil
	}
	for _, log := range logs {
		cp := *log
		s.logs = append(s.logs, &cp)
	}
	return nil
}

func TestBidLogWorkerPersistsAndAcksBatch(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2026, 6, 4, 12, 30, 0, 123000000, time.UTC)
	reader := &fakeBidLogStreamReader{messages: []itemcache.BidLogStreamMessage{
		{
			ID: "stream-1",
			Event: itemcache.BidLogEvent{
				BidID:           "bid_1",
				ItemID:          "item_1",
				RoomID:          "room_1",
				UserID:          "user_1",
				Price:           1200,
				CreatedAtUnixMS: createdAt.UnixMilli(),
			},
		},
	}}
	store := &fakeBidLogBatchStore{}
	worker := newBidLogWorker(store, reader, bidLogWorkerConfig{BatchSize: 20})

	if err := worker.drainOnce(ctx); err != nil {
		t.Fatalf("drainOnce returned error: %v", err)
	}

	if len(store.logs) != 1 {
		t.Fatalf("expected 1 bid log, got %d", len(store.logs))
	}
	got := store.logs[0]
	if got.ID != "bid_1" || got.ItemID != "item_1" || got.RoomID != "room_1" || got.UserID != "user_1" || got.Price != 1200 {
		t.Fatalf("bid log fields not copied: %+v", got)
	}
	if got.AuthorityEpoch != 0 || got.AuctionVersion != 0 {
		t.Fatalf("expected default authority fields, got %+v", got)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected CreatedAt %s, got %s", createdAt, got.CreatedAt)
	}
	if !reflect.DeepEqual(reader.acks, []string{"stream-1"}) {
		t.Fatalf("expected ack of stream message ID, got %#v", reader.acks)
	}
}

func TestBidLogWorkerAcksDuplicateAlreadyPersistedLogs(t *testing.T) {
	reader := &fakeBidLogStreamReader{messages: []itemcache.BidLogStreamMessage{{
		ID: "1-0",
		Event: itemcache.BidLogEvent{
			BidID: "bid_1", ItemID: "item_1", RoomID: "room_1", UserID: "user_1",
			Price: 1000, CreatedAtUnixMS: time.Now().UnixMilli(), AuthorityEpoch: 2, AuctionVersion: 1,
		},
	}}}
	store := &fakeBidLogBatchStore{duplicateOK: true}
	worker := newBidLogWorker(store, reader, bidLogWorkerConfig{BatchSize: 1})

	if err := worker.drainOnce(context.Background()); err != nil {
		t.Fatalf("drainOnce() error = %v", err)
	}
	if len(reader.acks) != 1 || reader.acks[0] != "1-0" {
		t.Fatalf("acked = %+v", reader.acks)
	}
}

func TestBidLogWorkerDoesNotAckWhenPersistFails(t *testing.T) {
	ctx := context.Background()
	reader := &fakeBidLogStreamReader{messages: []itemcache.BidLogStreamMessage{
		{
			ID: "stream-1",
			Event: itemcache.BidLogEvent{
				BidID:           "bid_1",
				ItemID:          "item_1",
				RoomID:          "room_1",
				UserID:          "user_1",
				Price:           1200,
				CreatedAtUnixMS: time.Now().UnixMilli(),
			},
		},
	}}
	storeErr := errors.New("store unavailable")
	store := &fakeBidLogBatchStore{err: storeErr}
	worker := newBidLogWorker(store, reader, bidLogWorkerConfig{})

	err := worker.drainOnce(ctx)
	if !errors.Is(err, storeErr) {
		t.Fatalf("expected store error, got %v", err)
	}
	if len(reader.acks) != 0 {
		t.Fatalf("expected no ack on persist failure, got %#v", reader.acks)
	}
}

func TestBidLogWorkerRecordsErrorWhenAckFails(t *testing.T) {
	ctx := context.Background()
	reader := &fakeBidLogStreamReader{
		ackErr: errors.New("ack unavailable"),
		messages: []itemcache.BidLogStreamMessage{
			{
				ID: "stream-1",
				Event: itemcache.BidLogEvent{
					BidID:           "bid_1",
					ItemID:          "item_1",
					RoomID:          "room_1",
					UserID:          "user_1",
					Price:           1200,
					CreatedAtUnixMS: time.Now().UnixMilli(),
				},
			},
		},
	}
	store := &fakeBidLogBatchStore{}
	rec := &bidLogWorkerCaptureRecorder{}
	observability.SetDefaultRecorder(rec)
	t.Cleanup(func() { observability.SetDefaultRecorder(nil) })
	worker := newBidLogWorker(store, reader, bidLogWorkerConfig{})

	err := worker.drainOnce(ctx)
	if err == nil {
		t.Fatal("expected ack error")
	}
	if len(rec.metrics) != 1 {
		t.Fatalf("expected one worker metric, got %d", len(rec.metrics))
	}
	if rec.metrics[0].Result != "error" {
		t.Fatalf("expected error worker metric, got %+v", rec.metrics[0])
	}
}

func TestBidLogWorkerEmptyBatchDoesNotPersistOrAck(t *testing.T) {
	ctx := context.Background()
	reader := &fakeBidLogStreamReader{}
	store := &fakeBidLogBatchStore{}
	worker := newBidLogWorker(store, reader, bidLogWorkerConfig{})

	if err := worker.drainOnce(ctx); err != nil {
		t.Fatalf("drainOnce returned error: %v", err)
	}
	if store.calls != 0 {
		t.Fatalf("expected no store call for empty batch, got %d", store.calls)
	}
	if len(reader.acks) != 0 {
		t.Fatalf("expected no ack for empty batch, got %#v", reader.acks)
	}
}

type bidLogWorkerCaptureRecorder struct {
	observability.NoopRecorder
	metrics []observability.BidLogWorkerMetric
}

func (r *bidLogWorkerCaptureRecorder) BidLogWorker(_ context.Context, metric observability.BidLogWorkerMetric) {
	r.metrics = append(r.metrics, metric)
}

func TestBidLogWorkerDrainsPendingBeforeNewMessages(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2026, 6, 4, 12, 30, 0, 0, time.UTC)
	reader := &fakeBidLogStreamReader{
		pendingMessages: []itemcache.BidLogStreamMessage{
			{
				ID: "pending-1",
				Event: itemcache.BidLogEvent{
					BidID:           "bid_pending",
					ItemID:          "item_1",
					RoomID:          "room_1",
					UserID:          "user_1",
					Price:           1300,
					CreatedAtUnixMS: createdAt.UnixMilli(),
				},
			},
		},
		messages: []itemcache.BidLogStreamMessage{
			{
				ID: "new-1",
				Event: itemcache.BidLogEvent{
					BidID:           "bid_new",
					ItemID:          "item_1",
					RoomID:          "room_1",
					UserID:          "user_2",
					Price:           1400,
					CreatedAtUnixMS: createdAt.UnixMilli(),
				},
			},
		},
	}
	store := &fakeBidLogBatchStore{}
	worker := newBidLogWorker(store, reader, bidLogWorkerConfig{})

	if err := worker.drainOnce(ctx); err != nil {
		t.Fatalf("drainOnce returned error: %v", err)
	}

	if reader.readPendingCalls != 1 {
		t.Fatalf("expected pending read before new read, got %d calls", reader.readPendingCalls)
	}
	if reader.readCalls != 0 {
		t.Fatalf("expected no new read when pending batch exists, got %d calls", reader.readCalls)
	}
	if len(store.logs) != 1 || store.logs[0].ID != "bid_pending" {
		t.Fatalf("expected pending bid log to be persisted first, got %#v", store.logs)
	}
	if !reflect.DeepEqual(reader.acks, []string{"pending-1"}) {
		t.Fatalf("expected pending message ack, got %#v", reader.acks)
	}
}
