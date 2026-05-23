package service

import (
	"errors"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/user/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/user/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/user/model"
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

func (s *Service) Register(input dto.RegisterInput) (*dto.LoginResult, error) {
	account := normalizeAccount(input.Account)
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

func (s *Service) Login(account string, password string) (*dto.LoginResult, error) {
	account = normalizeAccount(account)
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
	return s.loginResult(u)
}

func (s *Service) Authenticate(token string) (*model.User, error) {
	userID, err := s.tokens.Verify(token, s.now())
	if err != nil {
		return nil, err
	}
	u, err := s.store.FindUserByID(userID)
	if errors.Is(err, errorx.ErrNotFound) {
		return nil, errorx.ErrUnauthorized
	}
	return u, err
}

func (s *Service) UpdateProfile(u *model.User, input dto.UpdateProfileInput) error {
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

func (s *Service) DeleteMe(u *model.User) error {
	if err := s.store.DeleteUser(u.ID); err != nil {
		if errors.Is(err, errorx.ErrNotFound) {
			return errorx.ErrNotFound
		}
		return err
	}
	return nil
}

func (s *Service) loginResult(u *model.User) (*dto.LoginResult, error) {
	token, err := s.tokens.Sign(u.ID, s.now())
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
