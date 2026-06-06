package room

import (
	"testing"

	itemapp "github.com/zet-plane/live-auction-backend/internal/app/item"
	"github.com/zet-plane/live-auction-backend/internal/app/room/service"
)

func TestItemModuleExportsRoomItemReaderContract(t *testing.T) {
	var reader service.ItemReader = itemapp.ItemReader
	_ = reader
}
