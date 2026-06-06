package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/user/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/user/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/crypto"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Options struct {
	TokenSecret string
	TokenTTL    time.Duration
}

type Service struct {
	store  dao.Store
	tokens tokenService
	now    func() time.Time
}

func NewService(store dao.Store, opts Options) *Service {
	return &Service{
		store:  store,
		tokens: newTokenService(opts.TokenSecret, opts.TokenTTL),
		now:    time.Now,
	}
}

func (s *Service) Register(ctx context.Context, input dto.RegisterInput) (result *dto.LoginResult, err error) {
	account := normalizeAccount(input.Account)
	defer observability.Track(ctx, "user.register", "account", account)(&err)

	password := strings.TrimSpace(input.Password)
	if !validAccount(account) || !validPassword(password) {
		return nil, errorx.ErrInvalidRequest
	}
	name := dao.DefaultName(account)
	if _, err := s.store.FindUserByAccount(account); err == nil {
		return nil, errorx.ErrInvalidRequest
	} else if !errors.Is(err, errorx.ErrNotFound) {
		return nil, err
	}

	u := &model.User{
		ID:       "user_" + snowflake.MakeUUID(),
		Account:  account,
		Name:     name,
		Password: crypto.PasswordGen(password, account),
		Identity: model.IdentityUser,
	}
	if err := s.store.CreateUser(u); err != nil {
		return nil, err
	}
	return s.loginResult(u)
}

func (s *Service) Login(ctx context.Context, account string, password string) (result *dto.LoginResult, err error) {
	account = normalizeAccount(account)
	var userID string
	finish := observability.Track(ctx, "user.login", "account", account)
	defer func() {
		finish(&err, "user_id", userID)
	}()

	password = strings.TrimSpace(password)
	if !validAccount(account) || !validPassword(password) {
		return nil, errorx.ErrInvalidRequest
	}

	u, err := s.store.FindUserByAccount(account)
	if err != nil {
		if errors.Is(err, errorx.ErrNotFound) {
			return nil, errorx.ErrUnauthorized
		}
		return nil, err
	}
	if !crypto.PasswordCompare(password, u.Password, account) {
		return nil, errorx.ErrUnauthorized
	}
	userID = u.ID
	return s.loginResult(u)
}

func (s *Service) Authenticate(ctx context.Context, token string) (result *model.User, err error) {
	var userID string
	finish := observability.Track(ctx, "user.authenticate")
	defer func() {
		finish(&err, "user_id", userID)
	}()

	userID, err = s.tokens.Verify(token, s.now())
	if err != nil {
		return nil, err
	}
	u, err := s.store.FindUserByID(userID)
	if errors.Is(err, errorx.ErrNotFound) {
		return nil, errorx.ErrUnauthorized
	}
	return u, err
}

func (s *Service) AuthenticateClaims(ctx context.Context, token string) (result *model.User, err error) {
	var userID string
	finish := observability.Track(ctx, "user.authenticate_claims")
	defer func() {
		finish(&err, "user_id", userID)
	}()

	claims, err := s.tokens.VerifyClaims(token, s.now())
	if err != nil {
		return nil, err
	}
	userID = claims.Subject
	if claims.Identity != "" {
		return &model.User{
			ID:       claims.Subject,
			Name:     claims.Name,
			Identity: claims.Identity,
		}, nil
	}

	u, err := s.store.FindUserByID(claims.Subject)
	if errors.Is(err, errorx.ErrNotFound) {
		return nil, errorx.ErrUnauthorized
	}
	return u, err
}

func (s *Service) UpdateProfile(ctx context.Context, u *model.User, input dto.UpdateProfileInput) (err error) {
	defer observability.Track(ctx, "user.update_profile",
		"user_id", userID(u),
		"has_name", input.Name != nil,
		"has_avatar_url", input.AvatarURL != nil,
		"has_motto", input.Motto != nil,
		"has_identity", input.Identity != nil,
	)(&err)

	if input.Name != nil {
		u.Name = strings.TrimSpace(*input.Name)
	}
	if input.AvatarURL != nil {
		u.AvatarURL = strings.TrimSpace(*input.AvatarURL)
	}
	if input.Motto != nil {
		u.Motto = strings.TrimSpace(*input.Motto)
	}
	if input.Identity != nil {
		if !isValidIdentity(*input.Identity) {
			return errorx.ErrInvalidIdentity
		}
		u.Identity = *input.Identity
	}
	return s.store.UpdateUser(u)
}

func (s *Service) DeleteMe(ctx context.Context, u *model.User) (err error) {
	defer observability.Track(ctx, "user.delete_me", "user_id", userID(u))(&err)

	if err := s.store.DeleteUser(u.ID); err != nil {
		if errors.Is(err, errorx.ErrNotFound) {
			return errorx.ErrNotFound
		}
		return err
	}
	return nil
}

func userID(u *model.User) string {
	if u == nil {
		return ""
	}
	return u.ID
}

func (s *Service) loginResult(u *model.User) (*dto.LoginResult, error) {
	token, err := s.tokens.SignUser(u, s.now())
	if err != nil {
		return nil, err
	}
	return &dto.LoginResult{
		Token: token,
		User:  dto.NewUserDTO(u),
	}, nil
}

func normalizeAccount(account string) string {
	return strings.TrimSpace(account)
}

func validAccount(account string) bool {
	return len(account) >= 3 && len(account) <= 64
}

func validPassword(password string) bool {
	return len(password) >= 6 && len(password) <= 72
}

func isValidIdentity(identity model.UserIdentity) bool {
	return identity == model.IdentityUser || identity == model.IdentityMerchant
}
