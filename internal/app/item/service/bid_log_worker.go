package service

import (
	"context"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

type bidLogStreamReader interface {
	ReadPending(ctx context.Context, count int) ([]itemcache.BidLogStreamMessage, error)
	Read(ctx context.Context, count int) ([]itemcache.BidLogStreamMessage, error)
	Ack(ctx context.Context, ids []string) error
}

type bidLogBatchStore interface {
	CreateBidLogs(logs []*itemmodel.BidLog) error
}

type bidLogWorkerConfig struct {
	BatchSize    int
	PollInterval time.Duration
}

type bidLogWorker struct {
	store        bidLogBatchStore
	reader       bidLogStreamReader
	batchSize    int
	pollInterval time.Duration
}

func newBidLogWorker(store bidLogBatchStore, reader bidLogStreamReader, cfg bidLogWorkerConfig) *bidLogWorker {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 100 * time.Millisecond
	}
	return &bidLogWorker{
		store:        store,
		reader:       reader,
		batchSize:    cfg.BatchSize,
		pollInterval: cfg.PollInterval,
	}
}

func (w *bidLogWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.drainOnce(ctx); err != nil {
				logx.Warnw("item.bid_log_worker drain failed", "err", err)
			}
		}
	}
}

func (w *bidLogWorker) drainOnce(ctx context.Context) error {
	messages, err := w.reader.ReadPending(ctx, w.batchSize)
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		messages, err = w.reader.Read(ctx, w.batchSize)
		if err != nil {
			return err
		}
	}
	if len(messages) == 0 {
		return nil
	}

	logs := make([]*itemmodel.BidLog, 0, len(messages))
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		event := message.Event
		logs = append(logs, &itemmodel.BidLog{
			ID:        event.BidID,
			ItemID:    event.ItemID,
			RoomID:    event.RoomID,
			UserID:    event.UserID,
			Price:     event.Price,
			CreatedAt: time.UnixMilli(event.CreatedAtUnixMS),
		})
		ids = append(ids, message.ID)
	}

	if err := w.store.CreateBidLogs(logs); err != nil {
		return err
	}
	return w.reader.Ack(ctx, ids)
}
