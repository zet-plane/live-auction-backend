package appInitialize

import "github.com/zet-plane/live-auction-backend/internal/app/item"

func init() {
	apps = append(apps, &item.Item{Name: "item"})
}
