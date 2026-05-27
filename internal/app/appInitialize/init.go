package appInitialize

import (
	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/base"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit"
	"github.com/zet-plane/live-auction-backend/internal/app/item"
	"github.com/zet-plane/live-auction-backend/internal/app/order"
	"github.com/zet-plane/live-auction-backend/internal/app/payment"
	"github.com/zet-plane/live-auction-backend/internal/app/room"
	"github.com/zet-plane/live-auction-backend/internal/app/user"
	"github.com/zet-plane/live-auction-backend/internal/app/ws"
)

var apps = []app.Module{
	&base.Base{Name: "base"},
	&user.User{Name: "user"},
	&ws.WS{Name: "ws"},
	&room.Room{Name: "room"},
	&order.Order{Name: "order"},
	&payment.Payment{Name: "payment"},
	&deposit.Deposit{Name: "deposit"},
	&item.Item{Name: "item"},
}

func GetApps() []app.Module {
	return apps
}
