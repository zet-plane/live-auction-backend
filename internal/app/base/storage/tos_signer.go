package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/zet-plane/live-auction-backend/config"
	baseservice "github.com/zet-plane/live-auction-backend/internal/app/base/service"
)

var ErrTOSDisabled = errors.New("tos storage disabled")
var ErrTOSConfig = errors.New("invalid tos storage config")

const publicReadACL = "public-read"

type tosClient interface {
	PreSignedPostSignature(context.Context, *tos.PreSingedPostSignatureInput) (*tos.PreSingedPostSignatureOutput, error)
}

type TOSPostSigner struct {
	client    tosClient
	bucket    string
	uploadURL string
}

func NewTOSPostSigner(cfg config.TOSStorage) (*TOSPostSigner, error) {
	if !cfg.Enabled {
		return nil, ErrTOSDisabled
	}
	if strings.TrimSpace(cfg.Region) == "" ||
		strings.TrimSpace(cfg.Endpoint) == "" ||
		strings.TrimSpace(cfg.Bucket) == "" ||
		strings.TrimSpace(cfg.AccessKeyID) == "" ||
		strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, ErrTOSConfig
	}
	client, err := tos.NewClientV2(
		cfg.Endpoint,
		tos.WithRegion(cfg.Region),
		tos.WithCredentials(tos.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey)),
	)
	if err != nil {
		return nil, err
	}
	return &TOSPostSigner{
		client:    client,
		bucket:    cfg.Bucket,
		uploadURL: fmt.Sprintf("https://%s.%s/", cfg.Bucket, strings.TrimPrefix(cfg.Endpoint, "https://")),
	}, nil
}

func (s *TOSPostSigner) SignImageUpload(ctx context.Context, input baseservice.SignInput) (baseservice.SignOutput, error) {
	out, err := s.client.PreSignedPostSignature(ctx, &tos.PreSingedPostSignatureInput{
		Bucket:  s.bucket,
		Key:     input.ObjectKey,
		Expires: int64(input.Expires.Seconds()),
		Conditions: []tos.PostSignatureCondition{
			{
				Key:   "Content-Type",
				Value: input.ContentType,
			},
			{
				Key:   "x-tos-acl",
				Value: publicReadACL,
			},
		},
		ContentLengthRange: &tos.ContentLengthRange{
			RangeStart: 1,
			RangeEnd:   input.Size,
		},
	})
	if err != nil {
		return baseservice.SignOutput{}, err
	}
	return baseservice.SignOutput{
		UploadURL: s.uploadURL,
		FormFields: postFieldsFromSignature(input.ObjectKey, input.ContentType, signatureOutput{
			Policy:     out.Policy,
			Algorithm:  out.Algorithm,
			Credential: out.Credential,
			Date:       out.Date,
			Signature:  out.Signature,
		}),
	}, nil
}

type signatureOutput struct {
	Policy     string
	Algorithm  string
	Credential string
	Date       string
	Signature  string
}

func postFieldsFromSignature(objectKey, contentType string, out signatureOutput) map[string]string {
	return map[string]string{
		"key":              objectKey,
		"Content-Type":     contentType,
		"policy":           out.Policy,
		"x-tos-algorithm":  out.Algorithm,
		"x-tos-credential": out.Credential,
		"x-tos-date":       out.Date,
		"x-tos-signature":  out.Signature,
		"x-tos-acl":        publicReadACL,
	}
}
