package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/base/dto"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

type fakeSigner struct {
	calls []SignInput
	err   error
}

func (f *fakeSigner) SignImageUpload(_ context.Context, input SignInput) (SignOutput, error) {
	f.calls = append(f.calls, input)
	if f.err != nil {
		return SignOutput{}, f.err
	}
	return SignOutput{
		UploadURL: "https://bucket.tos-cn-beijing.volces.com/",
		FormFields: map[string]string{
			"key":             input.ObjectKey,
			"policy":          "policy",
			"x-tos-signature": "signature",
		},
	}, nil
}

func TestSignImageUploadCreatesExpectedObjectKeyAndSignerInput(t *testing.T) {
	fake := &fakeSigner{}
	svc := newTestUploadService(fake)
	current := &usermodel.User{ID: "user_123"}

	result, err := svc.SignImageUpload(context.Background(), current, dto.SignImageUploadInput{
		Filename:    "cover.png",
		ContentType: "image/png",
		Size:        123456,
		Usage:       "item",
	})
	if err != nil {
		t.Fatalf("SignImageUpload error = %v", err)
	}

	wantKey := "uploads/images/item/user_123/2026/06/img_fixed.png"
	if result.ObjectKey != wantKey {
		t.Fatalf("object key = %q, want %q", result.ObjectKey, wantKey)
	}
	if result.ImageURL != "https://img.example.com/"+wantKey {
		t.Fatalf("image url = %q", result.ImageURL)
	}
	if result.UploadURL != "https://bucket.tos-cn-beijing.volces.com/" {
		t.Fatalf("upload url = %q", result.UploadURL)
	}
	if result.ExpiresIn != 600 {
		t.Fatalf("expires_in = %d, want 600", result.ExpiresIn)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("signer calls = %d, want 1", len(fake.calls))
	}
	if fake.calls[0].ObjectKey != wantKey {
		t.Fatalf("signed object key = %q, want %q", fake.calls[0].ObjectKey, wantKey)
	}
	if fake.calls[0].ContentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", fake.calls[0].ContentType)
	}
	if fake.calls[0].Size != 123456 {
		t.Fatalf("size = %d, want 123456", fake.calls[0].Size)
	}
	if fake.calls[0].Expires != 10*time.Minute {
		t.Fatalf("expires = %v, want 10m", fake.calls[0].Expires)
	}
}

func TestSignImageUploadRejectsInvalidContentType(t *testing.T) {
	fake := &fakeSigner{}
	svc := newTestUploadService(fake)

	_, err := svc.SignImageUpload(context.Background(), &usermodel.User{ID: "user_123"}, dto.SignImageUploadInput{
		Filename:    "cover.png",
		ContentType: "text/plain",
		Size:        123456,
		Usage:       "item",
	})
	if err != errorx.ErrInvalidRequest {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("signer calls = %d, want 0", len(fake.calls))
	}
}

func TestSignImageUploadRejectsExtensionMismatch(t *testing.T) {
	fake := &fakeSigner{}
	svc := newTestUploadService(fake)

	_, err := svc.SignImageUpload(context.Background(), &usermodel.User{ID: "user_123"}, dto.SignImageUploadInput{
		Filename:    "cover.jpg",
		ContentType: "image/png",
		Size:        123456,
		Usage:       "item",
	})
	if err != errorx.ErrInvalidRequest {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("signer calls = %d, want 0", len(fake.calls))
	}
}

func TestSignImageUploadRejectsOversizedFileBeforeSigner(t *testing.T) {
	fake := &fakeSigner{}
	svc := newTestUploadService(fake)

	_, err := svc.SignImageUpload(context.Background(), &usermodel.User{ID: "user_123"}, dto.SignImageUploadInput{
		Filename:    "cover.webp",
		ContentType: "image/webp",
		Size:        10*1024*1024 + 1,
		Usage:       "item",
	})
	if err != errorx.ErrInvalidRequest {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("signer calls = %d, want 0", len(fake.calls))
	}
}

func TestSignImageUploadRejectsInvalidUsage(t *testing.T) {
	fake := &fakeSigner{}
	svc := newTestUploadService(fake)

	_, err := svc.SignImageUpload(context.Background(), &usermodel.User{ID: "user_123"}, dto.SignImageUploadInput{
		Filename:    "cover.png",
		ContentType: "image/png",
		Size:        123456,
		Usage:       "order",
	})
	if err != errorx.ErrInvalidRequest {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("signer calls = %d, want 0", len(fake.calls))
	}
}

func TestSignImageUploadReturnsInternalWhenSignerFails(t *testing.T) {
	fake := &fakeSigner{err: errors.New("sign failed")}
	svc := newTestUploadService(fake)

	_, err := svc.SignImageUpload(context.Background(), &usermodel.User{ID: "user_123"}, dto.SignImageUploadInput{
		Filename:    "cover.png",
		ContentType: "image/png",
		Size:        123456,
		Usage:       "item",
	})
	if err != errorx.ErrInternal {
		t.Fatalf("error = %v, want ErrInternal", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("signer calls = %d, want 1", len(fake.calls))
	}
}

func newTestUploadService(fake *fakeSigner) *UploadService {
	return NewUploadService(fake, Options{
		PublicBaseURL: "https://img.example.com",
		MaxSizeBytes:  10 * 1024 * 1024,
		Expires:       10 * time.Minute,
		Now:           func() time.Time { return time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC) },
		NewID:         func() string { return "img_fixed" },
	})
}
