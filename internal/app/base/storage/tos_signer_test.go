package storage

import (
	"context"
	"reflect"
	"testing"
	"time"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/zet-plane/live-auction-backend/config"
	baseservice "github.com/zet-plane/live-auction-backend/internal/app/base/service"
)

func TestNewTOSPostSignerRejectsDisabledConfig(t *testing.T) {
	_, err := NewTOSPostSigner(config.TOSStorage{})
	if err != ErrTOSDisabled {
		t.Fatalf("error = %v, want ErrTOSDisabled", err)
	}
}

func TestNewTOSPostSignerRejectsMissingRequiredConfig(t *testing.T) {
	_, err := NewTOSPostSigner(config.TOSStorage{
		Enabled: true,
		Region:  "cn-beijing",
	})
	if err != ErrTOSConfig {
		t.Fatalf("error = %v, want ErrTOSConfig", err)
	}
}

func TestPostFieldsFromSignatureMapsTOSFields(t *testing.T) {
	fields := postFieldsFromSignature("uploads/images/item/user_123/2026/06/img.png", "image/png", signatureOutput{
		Policy:     "policy",
		Algorithm:  "TOS4-HMAC-SHA256",
		Credential: "ak/20260608/cn-beijing/tos/request",
		Date:       "20260608T120000Z",
		Signature:  "sig",
	})

	want := map[string]string{
		"key":              "uploads/images/item/user_123/2026/06/img.png",
		"Content-Type":     "image/png",
		"policy":           "policy",
		"x-tos-algorithm":  "TOS4-HMAC-SHA256",
		"x-tos-credential": "ak/20260608/cn-beijing/tos/request",
		"x-tos-date":       "20260608T120000Z",
		"x-tos-signature":  "sig",
		"x-tos-acl":        "public-read",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields = %#v, want %#v", fields, want)
	}
}

func TestSignImageUploadIncludesPublicReadACLCondition(t *testing.T) {
	client := &fakeTOSClient{}
	signer := &TOSPostSigner{
		client:    client,
		bucket:    "live-auction",
		uploadURL: "https://live-auction.tos-cn-beijing.volces.com/",
	}

	out, err := signer.SignImageUpload(context.Background(), baseservice.SignInput{
		ObjectKey:   "uploads/images/general/user_123/2026/06/img.png",
		ContentType: "image/png",
		Size:        123,
		Expires:     10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("SignImageUpload error = %v", err)
	}
	if out.FormFields["x-tos-acl"] != "public-read" {
		t.Fatalf("x-tos-acl field = %q, want public-read", out.FormFields["x-tos-acl"])
	}
	if !hasCondition(client.input.Conditions, "x-tos-acl", "public-read") {
		t.Fatalf("conditions = %#v, want x-tos-acl public-read", client.input.Conditions)
	}
}

type fakeTOSClient struct {
	input *tos.PreSingedPostSignatureInput
}

func (f *fakeTOSClient) PreSignedPostSignature(_ context.Context, input *tos.PreSingedPostSignatureInput) (*tos.PreSingedPostSignatureOutput, error) {
	f.input = input
	return &tos.PreSingedPostSignatureOutput{
		Policy:     "policy",
		Algorithm:  "TOS4-HMAC-SHA256",
		Credential: "ak/20260608/cn-beijing/tos/request",
		Date:       "20260608T120000Z",
		Signature:  "sig",
	}, nil
}

func hasCondition(conditions []tos.PostSignatureCondition, key, value string) bool {
	for _, condition := range conditions {
		if condition.Key == key && condition.Value == value {
			return true
		}
	}
	return false
}
