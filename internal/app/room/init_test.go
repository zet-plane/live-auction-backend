package room

import (
	"testing"

	itemapp "github.com/zet-plane/live-auction-backend/internal/app/item"
	"github.com/zet-plane/live-auction-backend/internal/app/room/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/room/service"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

func TestItemModuleExportsRoomItemReaderContract(t *testing.T) {
	var reader service.ItemReader = itemapp.ItemReader
	_ = reader
}

func TestCacheForEngineUsesActiveRedisWhenAvailabilityConfigured(t *testing.T) {
	var rt availability.Runtime
	c := cacheForEngine(&kernel.Engine{Availability: &rt})
	if _, ok := c.(*cache.ActiveRedisCache); !ok {
		t.Fatalf("cacheForEngine() = %T, want *cache.ActiveRedisCache", c)
	}
}
