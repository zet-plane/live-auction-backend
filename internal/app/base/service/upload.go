package service

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/base/dto"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

const defaultImageMaxSizeBytes int64 = 10 * 1024 * 1024

type Signer interface {
	SignImageUpload(ctx context.Context, input SignInput) (SignOutput, error)
}

type SignInput struct {
	ObjectKey   string
	ContentType string
	Size        int64
	Expires     time.Duration
}

type SignOutput struct {
	UploadURL  string
	FormFields map[string]string
}

type Options struct {
	PublicBaseURL string
	MaxSizeBytes  int64
	Expires       time.Duration
	Now           func() time.Time
	NewID         func() string
}

type UploadService struct {
	signer        Signer
	publicBaseURL string
	maxSizeBytes  int64
	expires       time.Duration
	now           func() time.Time
	newID         func() string
}

func NewUploadService(signer Signer, opts Options) *UploadService {
	if opts.MaxSizeBytes <= 0 {
		opts.MaxSizeBytes = defaultImageMaxSizeBytes
	}
	if opts.Expires <= 0 {
		opts.Expires = 10 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NewID == nil {
		opts.NewID = func() string { return "img_" + snowflake.MakeUUID() }
	}
	return &UploadService{
		signer:        signer,
		publicBaseURL: strings.TrimRight(opts.PublicBaseURL, "/"),
		maxSizeBytes:  opts.MaxSizeBytes,
		expires:       opts.Expires,
		now:           opts.Now,
		newID:         opts.NewID,
	}
}

func (s *UploadService) SignImageUpload(ctx context.Context, current *usermodel.User, input dto.SignImageUploadInput) (dto.SignImageUploadResult, error) {
	if s == nil || s.signer == nil || current == nil {
		return dto.SignImageUploadResult{}, errorx.ErrInternal
	}
	contentType, ext, err := normalizeImage(input.Filename, input.ContentType)
	if err != nil {
		return dto.SignImageUploadResult{}, err
	}
	if input.Size <= 0 || input.Size > s.maxSizeBytes {
		return dto.SignImageUploadResult{}, errorx.ErrInvalidRequest
	}
	usage := strings.TrimSpace(input.Usage)
	if usage != "item" && usage != "avatar" && usage != "general" {
		return dto.SignImageUploadResult{}, errorx.ErrInvalidRequest
	}
	objectKey := s.objectKey(current.ID, usage, ext)
	signed, err := s.signer.SignImageUpload(ctx, SignInput{
		ObjectKey:   objectKey,
		ContentType: contentType,
		Size:        input.Size,
		Expires:     s.expires,
	})
	if err != nil {
		logx.Warnw("SignImageUpload signer failed", "user_id", current.ID, "err", err)
		return dto.SignImageUploadResult{}, errorx.ErrInternal
	}
	return dto.SignImageUploadResult{
		UploadURL:  signed.UploadURL,
		FormFields: signed.FormFields,
		ImageURL:   s.publicURL(objectKey),
		ObjectKey:  objectKey,
		ExpiresIn:  int64(s.expires.Seconds()),
	}, nil
}

func (s *UploadService) objectKey(userID, usage, ext string) string {
	now := s.now()
	return path.Join("uploads", "images", usage, userID, now.Format("2006"), now.Format("01"), s.newID()+"."+ext)
}

func (s *UploadService) publicURL(objectKey string) string {
	if s.publicBaseURL == "" {
		return objectKey
	}
	return s.publicBaseURL + "/" + objectKey
}

func normalizeImage(filename, contentType string) (string, string, error) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(strings.TrimSpace(filename)), "."))
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ext == "" {
		return "", "", errorx.ErrInvalidRequest
	}
	allowed := map[string]string{
		"jpg":  "image/jpeg",
		"jpeg": "image/jpeg",
		"png":  "image/png",
		"webp": "image/webp",
	}
	want, ok := allowed[ext]
	if !ok || ct != want {
		return "", "", errorx.ErrInvalidRequest
	}
	if ext == "jpeg" {
		ext = "jpg"
	}
	return ct, ext, nil
}
