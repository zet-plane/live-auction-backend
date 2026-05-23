package appInitialize

import "github.com/zet-plane/live-auction-backend/internal/app/room"

func init() {
	apps = append(apps, &room.Room{Name: "room"})
}
