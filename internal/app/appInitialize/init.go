package appInitialize

import (
	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/item"
	"github.com/zet-plane/live-auction-backend/internal/app/order"
	"github.com/zet-plane/live-auction-backend/internal/app/payment"
	"github.com/zet-plane/live-auction-backend/internal/app/room"
	"github.com/zet-plane/live-auction-backend/internal/app/user"
)

var apps = []app.Module{
	&user.User{Name: "user"},
	&room.Room{Name: "room"},
	&order.Order{Name: "order"},
	&payment.Payment{Name: "payment"},
	&item.Item{Name: "item"},
}

func GetApps() []app.Module {
	return apps
}
