package appInitialize

import "github.com/zet-plane/live-auction-backend/internal/app"

var apps = make([]app.Module, 0)

func GetApps() []app.Module {
	return apps
}
