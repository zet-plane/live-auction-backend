package appInitialize

import "github.com/zet-plane/live-auction-backend/internal/app/user"

func init() {
	apps = append(apps, &user.User{Name: "user"})
}
