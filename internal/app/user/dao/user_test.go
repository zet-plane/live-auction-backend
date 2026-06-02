package dao

import (
	"testing"

	"github.com/zet-plane/live-auction-backend/internal/app/user/model"
)

func TestUserProfileUpdateValuesOnlyContainsMutableProfileFields(t *testing.T) {
	values := userProfileUpdateValues(&model.User{
		ID:        "user_1",
		Account:   "alice",
		Name:      "Alice",
		AvatarURL: "https://example.com/a.png",
		Password:  "hashed-password",
		Motto:     "hello",
		Identity:  model.IdentityMerchant,
	})

	for _, forbidden := range []string{"id", "account", "password", "deleted_at", "created_at", "updated_at"} {
		if _, ok := values[forbidden]; ok {
			t.Fatalf("profile update values must not include %s", forbidden)
		}
	}
	for _, required := range []string{"name", "avatar_url", "motto", "identity"} {
		if _, ok := values[required]; !ok {
			t.Fatalf("profile update values should include %s", required)
		}
	}
}
