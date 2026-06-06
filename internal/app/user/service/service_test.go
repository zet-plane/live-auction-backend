package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/user/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

type fakeStore struct {
	usersByAccount    map[string]*model.User
	usersByID         map[string]*model.User
	findUserByIDCalls int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		usersByAccount: map[string]*model.User{},
		usersByID:      map[string]*model.User{},
	}
}

func (s *fakeStore) AutoMigrate() error { return nil }

func (s *fakeStore) CreateUser(u *model.User) error {
	if _, exists := s.usersByAccount[u.Account]; exists {
		return errorx.ErrInvalidRequest
	}
	copy := *u
	s.usersByAccount[u.Account] = &copy
	s.usersByID[u.ID] = &copy
	return nil
}

func (s *fakeStore) FindUserByAccount(account string) (*model.User, error) {
	u, ok := s.usersByAccount[account]
	if !ok {
		return nil, errorx.ErrNotFound
	}
	copy := *u
	return &copy, nil
}

func (s *fakeStore) FindUserByID(id string) (*model.User, error) {
	s.findUserByIDCalls++
	u, ok := s.usersByID[id]
	if !ok {
		return nil, errorx.ErrNotFound
	}
	copy := *u
	return &copy, nil
}

func (s *fakeStore) UpdateUser(u *model.User) error {
	copy := *u
	s.usersByAccount[u.Account] = &copy
	s.usersByID[u.ID] = &copy
	return nil
}

func (s *fakeStore) DeleteUser(id string) error {
	u, ok := s.usersByID[id]
	if !ok {
		return errorx.ErrNotFound
	}
	delete(s.usersByID, id)
	delete(s.usersByAccount, u.Account)
	return nil
}

func TestRegisterCreatesAccountUserWithDefaultNameAndReturnsToken(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, Options{TokenSecret: "test-secret", TokenTTL: time.Hour})

	result, err := svc.Register(context.Background(), dto.RegisterInput{
		Account:  " alice ",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected token")
	}
	if result.User.Account != "alice" {
		t.Fatalf("expected normalized account alice, got %q", result.User.Account)
	}
	if result.User.Name != "Userlice" {
		t.Fatalf("expected default name Userlice, got %q", result.User.Name)
	}

	stored := store.usersByAccount["alice"]
	if stored == nil {
		t.Fatal("expected stored user")
	}
	if stored.Password == "password123" {
		t.Fatal("expected stored password to be hashed")
	}
}

func TestRegisterRejectsAccountShorterThanThree(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, Options{TokenSecret: "test-secret", TokenTTL: time.Hour})

	if _, err := svc.Register(context.Background(), dto.RegisterInput{
		Account:  "ab",
		Password: "password123",
	}); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request, got %v", err)
	}
	if _, exists := store.usersByAccount["ab"]; exists {
		t.Fatal("expected short account not to be stored")
	}
}

func TestRegisterRejectsPasswordOutsideLengthBounds(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{name: "too short", password: "12345"},
		{name: "too long", password: "1234567890123456789012345678901234567890123456789012345678901234567890123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			svc := NewService(store, Options{TokenSecret: "test-secret", TokenTTL: time.Hour})

			if _, err := svc.Register(context.Background(), dto.RegisterInput{
				Account:  "alice",
				Password: tt.password,
			}); !errors.Is(err, errorx.ErrInvalidRequest) {
				t.Fatalf("expected invalid request, got %v", err)
			}
			if _, exists := store.usersByAccount["alice"]; exists {
				t.Fatal("expected invalid password not to be stored")
			}
		})
	}
}

func TestLoginUsesAccountPassword(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, Options{TokenSecret: "test-secret", TokenTTL: time.Hour})
	if _, err := svc.Register(context.Background(), dto.RegisterInput{
		Account:  "alice",
		Password: "password123",
	}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	result, err := svc.Login(context.Background(), "alice", "password123")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected token")
	}
	if result.User.Account != "alice" {
		t.Fatalf("expected user account alice, got %q", result.User.Account)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, Options{TokenSecret: "test-secret", TokenTTL: time.Hour})
	if _, err := svc.Register(context.Background(), dto.RegisterInput{
		Account:  "alice",
		Password: "password123",
	}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if _, err := svc.Login(context.Background(), "alice", "wrong-password"); !errors.Is(err, errorx.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestAuthenticateClaimsUsesTokenClaimsWithoutStoreLookup(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, Options{TokenSecret: "test-secret", TokenTTL: time.Hour})
	result, err := svc.Register(context.Background(), dto.RegisterInput{
		Account:  "alice",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	store.findUserByIDCalls = 0
	current, err := svc.AuthenticateClaims(context.Background(), result.Token)
	if err != nil {
		t.Fatalf("AuthenticateClaims returned error: %v", err)
	}
	if current.ID != result.User.ID || current.Name != result.User.Name || current.Identity != result.User.Identity {
		t.Fatalf("expected current user from token claims, got %+v from %+v", current, result.User)
	}
	if store.findUserByIDCalls != 0 {
		t.Fatalf("expected no FindUserByID calls, got %d", store.findUserByIDCalls)
	}
}

func TestAuthenticateClaimsFallsBackToStoreForLegacyToken(t *testing.T) {
	store := newFakeStore()
	user := &model.User{
		ID:       "user_legacy",
		Account:  "legacy",
		Name:     "Legacy User",
		Password: "hashed",
		Identity: model.IdentityUser,
	}
	if err := store.CreateUser(user); err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	svc := NewService(store, Options{TokenSecret: "test-secret", TokenTTL: time.Hour})
	token, err := svc.tokens.Sign(user.ID, svc.now())
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}

	store.findUserByIDCalls = 0
	current, err := svc.AuthenticateClaims(context.Background(), token)
	if err != nil {
		t.Fatalf("AuthenticateClaims returned error: %v", err)
	}
	if current.ID != user.ID || current.Name != user.Name || current.Identity != user.Identity {
		t.Fatalf("expected current user from store fallback, got %+v", current)
	}
	if store.findUserByIDCalls != 1 {
		t.Fatalf("expected one FindUserByID fallback call, got %d", store.findUserByIDCalls)
	}
}
