package storage

import (
	"reflect"
	"testing"

	"github.com/zet-plane/live-auction-backend/config"
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
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields = %#v, want %#v", fields, want)
	}
}
