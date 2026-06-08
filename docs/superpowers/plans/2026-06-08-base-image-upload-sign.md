# Base Image Upload Sign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an authenticated `base` module endpoint that returns short-lived Volcengine TOS browser POST upload signatures for images.

**Architecture:** Keep upload signing in `internal/app/base` so existing item/user modules only continue storing image URLs. The base service validates requested image metadata, generates object keys, and delegates signing to a small interface; the TOS SDK stays behind a storage adapter so service tests use a fake signer and never touch the network.

**Tech Stack:** Go, Flamego, existing `response`/`web.Authorization` middleware, `pkg/errorx`, `pkg/snowflake`, Viper-backed YAML config, `github.com/volcengine/ve-tos-golang-sdk/v2/tos`.

---

## File Structure

- Modify `config/vars.go`: add `Storage` and nested `TOS` config structs.
- Modify `config/config.go`: add helpers for upload expiry and image size fallbacks.
- Modify `config/config_test.go`: cover config helper defaults and overrides.
- Modify `config.yaml.example`: add non-secret TOS example fields with empty credentials.
- Create `internal/app/base/dto/upload.go`: request/response DTOs for signing.
- Create `internal/app/base/service/upload.go`: validation, object key generation, public URL construction, signer interface.
- Create `internal/app/base/service/upload_test.go`: unit tests with fake signer, fixed time, and fixed ID maker.
- Create `internal/app/base/storage/tos_signer.go`: Volcengine TOS SDK adapter for POST policy signatures.
- Create `internal/app/base/storage/tos_signer_test.go`: local tests for config validation and form field mapping without external calls.
- Modify `internal/app/base/handler/health.go`: add upload service injection and `SignImageUpload` handler.
- Modify `internal/app/base/handler/health_test.go`: add handler tests for binding/service wiring.
- Modify `internal/app/base/router/router.go`: register authenticated `POST /api/v1/base/uploads/images/sign`.
- Modify `internal/app/base/init.go`: construct signer/service from config.
- Modify `go.mod` / `go.sum`: require Volcengine TOS SDK if it is not already a direct dependency.

### Task 1: Add Storage Config

**Files:**
- Modify: `config/vars.go`
- Modify: `config/config.go`
- Modify: `config/config_test.go`
- Modify: `config.yaml.example`

- [ ] **Step 1: Write the failing config tests**

Add these tests to `config/config_test.go`:

```go
func TestStorageTOSUploadExpires(t *testing.T) {
	cfg := &GlobalConfig{}
	if got := cfg.StorageTOSUploadExpires(); got != 10*time.Minute {
		t.Fatalf("default upload expires = %v, want 10m", got)
	}

	cfg.Storage.TOS.UploadExpires = "5m"
	if got := cfg.StorageTOSUploadExpires(); got != 5*time.Minute {
		t.Fatalf("configured upload expires = %v, want 5m", got)
	}

	cfg.Storage.TOS.UploadExpires = "bad"
	if got := cfg.StorageTOSUploadExpires(); got != 10*time.Minute {
		t.Fatalf("bad upload expires fallback = %v, want 10m", got)
	}
}

func TestStorageTOSImageMaxSizeBytes(t *testing.T) {
	cfg := &GlobalConfig{}
	if got := cfg.StorageTOSImageMaxSizeBytes(); got != 10*1024*1024 {
		t.Fatalf("default max size = %d, want 10485760", got)
	}

	cfg.Storage.TOS.ImageMaxSizeBytes = 2 * 1024 * 1024
	if got := cfg.StorageTOSImageMaxSizeBytes(); got != 2*1024*1024 {
		t.Fatalf("configured max size = %d, want 2097152", got)
	}

	cfg.Storage.TOS.ImageMaxSizeBytes = -1
	if got := cfg.StorageTOSImageMaxSizeBytes(); got != 10*1024*1024 {
		t.Fatalf("negative max size fallback = %d, want 10485760", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `rtk go test ./config -run 'TestStorageTOS'`

Expected: FAIL because `GlobalConfig.StorageTOSUploadExpires` and `GlobalConfig.StorageTOSImageMaxSizeBytes` are undefined.

- [ ] **Step 3: Add config structs and helpers**

In `config/vars.go`, add `Storage Storage` to `GlobalConfig`:

```go
Storage      Storage      `yaml:"storage"       mapstructure:"storage"`
```

Add these structs in the same file:

```go
type Storage struct {
	TOS TOSStorage `yaml:"tos" mapstructure:"tos"`
}

type TOSStorage struct {
	Enabled           bool   `yaml:"enabled"              mapstructure:"enabled"`
	Region            string `yaml:"region"               mapstructure:"region"`
	Endpoint          string `yaml:"endpoint"             mapstructure:"endpoint"`
	Bucket            string `yaml:"bucket"               mapstructure:"bucket"`
	AccessKeyID       string `yaml:"access_key_id"        mapstructure:"access_key_id"`
	SecretAccessKey   string `yaml:"secret_access_key"    mapstructure:"secret_access_key"`
	PublicBaseURL     string `yaml:"public_base_url"      mapstructure:"public_base_url"`
	UploadExpires     string `yaml:"upload_expires"       mapstructure:"upload_expires"`
	ImageMaxSizeBytes int64  `yaml:"image_max_size_bytes" mapstructure:"image_max_size_bytes"`
}
```

In `config/config.go`, add:

```go
func (c *GlobalConfig) StorageTOSUploadExpires() time.Duration {
	return parseDuration(c.Storage.TOS.UploadExpires, 10*time.Minute)
}

func (c *GlobalConfig) StorageTOSImageMaxSizeBytes() int64 {
	if c.Storage.TOS.ImageMaxSizeBytes <= 0 {
		return 10 * 1024 * 1024
	}
	return c.Storage.TOS.ImageMaxSizeBytes
}
```

- [ ] **Step 4: Add example YAML**

Append this section to `config.yaml.example`:

```yaml
storage:
  tos:
    enabled: false
    region: cn-beijing
    endpoint: tos-cn-beijing.volces.com
    bucket: live-auction-images
    access_key_id: ""
    secret_access_key: ""
    public_base_url: https://img.example.com
    upload_expires: 10m
    image_max_size_bytes: 10485760
```

- [ ] **Step 5: Verify and commit**

Run: `rtk go test ./config -run 'TestStorageTOS|TestObservabilityMetricsInterval'`

Expected: PASS.

Commit:

```bash
rtk git add config/vars.go config/config.go config/config_test.go config.yaml.example
rtk git commit -m "feat(config): add tos image upload settings"
```

### Task 2: Add Base Upload Service

**Files:**
- Create: `internal/app/base/dto/upload.go`
- Create: `internal/app/base/service/upload.go`
- Create: `internal/app/base/service/upload_test.go`

- [ ] **Step 1: Add DTO types**

Create `internal/app/base/dto/upload.go`:

```go
package dto

type SignImageUploadRequest struct {
	Filename    string `json:"filename"     binding:"required,min=1,max=255"`
	ContentType string `json:"content_type" binding:"required,min=1,max=128"`
	Size        int64  `json:"size"         binding:"required,min=1"`
	Usage       string `json:"usage"        binding:"required,oneof=item avatar general"`
}

type SignImageUploadInput struct {
	Filename    string
	ContentType string
	Size        int64
	Usage       string
}

func (r SignImageUploadRequest) Input() SignImageUploadInput {
	return SignImageUploadInput{
		Filename:    r.Filename,
		ContentType: r.ContentType,
		Size:        r.Size,
		Usage:       r.Usage,
	}
}

type SignImageUploadResult struct {
	UploadURL  string            `json:"upload_url"`
	FormFields map[string]string `json:"form_fields"`
	ImageURL   string            `json:"image_url"`
	ObjectKey  string            `json:"object_key"`
	ExpiresIn  int64             `json:"expires_in"`
}
```

- [ ] **Step 2: Write failing service tests**

Create `internal/app/base/service/upload_test.go` with tests named:

```go
func TestSignImageUploadCreatesExpectedObjectKeyAndSignerInput(t *testing.T)
func TestSignImageUploadRejectsInvalidContentType(t *testing.T)
func TestSignImageUploadRejectsExtensionMismatch(t *testing.T)
func TestSignImageUploadRejectsOversizedFileBeforeSigner(t *testing.T)
func TestSignImageUploadRejectsInvalidUsage(t *testing.T)
func TestSignImageUploadReturnsInternalWhenSignerFails(t *testing.T)
```

Use this fake signer shape:

```go
type fakeSigner struct {
	calls []SignInput
	err   error
}

func (f *fakeSigner) SignImageUpload(ctx context.Context, input SignInput) (SignOutput, error) {
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
```

Create the service in tests with:

```go
svc := NewUploadService(fake, Options{
	PublicBaseURL: "https://img.example.com",
	MaxSizeBytes:  10 * 1024 * 1024,
	Expires:       10 * time.Minute,
	Now:           func() time.Time { return time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC) },
	NewID:          func() string { return "img_fixed" },
})
```

Assert the valid case returns:

```go
wantKey := "uploads/images/item/user_123/2026/06/img_fixed.png"
if result.ObjectKey != wantKey {
	t.Fatalf("object key = %q, want %q", result.ObjectKey, wantKey)
}
if result.ImageURL != "https://img.example.com/"+wantKey {
	t.Fatalf("image url = %q", result.ImageURL)
}
if result.ExpiresIn != 600 {
	t.Fatalf("expires_in = %d, want 600", result.ExpiresIn)
}
if len(fake.calls) != 1 {
	t.Fatalf("signer calls = %d, want 1", len(fake.calls))
}
if fake.calls[0].ContentType != "image/png" {
	t.Fatalf("content type = %q, want image/png", fake.calls[0].ContentType)
}
if fake.calls[0].Size != 123456 {
	t.Fatalf("size = %d, want 123456", fake.calls[0].Size)
}
```

For invalid cases, assert `errors.Is` is not required; compare returned error to `errorx.ErrInvalidRequest` for validation errors, and ensure `len(fake.calls) == 0`.

- [ ] **Step 3: Run service tests to verify they fail**

Run: `rtk go test ./internal/app/base/service`

Expected: FAIL because the service package and types are missing.

- [ ] **Step 4: Implement the upload service**

Create `internal/app/base/service/upload.go`:

```go
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
```

- [ ] **Step 5: Verify and commit**

Run: `rtk go test ./internal/app/base/service`

Expected: PASS.

Commit:

```bash
rtk git add internal/app/base/dto/upload.go internal/app/base/service/upload.go internal/app/base/service/upload_test.go
rtk git commit -m "feat(base): add image upload signing service"
```

### Task 3: Add Volcengine TOS Signer Adapter

**Files:**
- Create: `internal/app/base/storage/tos_signer.go`
- Create: `internal/app/base/storage/tos_signer_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Write adapter tests**

Create `internal/app/base/storage/tos_signer_test.go` with tests named:

```go
func TestNewTOSPostSignerRejectsDisabledConfig(t *testing.T)
func TestNewTOSPostSignerRejectsMissingRequiredConfig(t *testing.T)
func TestPostFieldsFromSignatureMapsTOSFields(t *testing.T)
```

The field mapping test should call an unexported helper:

```go
fields := postFieldsFromSignature("uploads/images/item/user_123/2026/06/img.png", "image/png", signatureOutput{
	Policy:     "policy",
	Algorithm:  "TOS4-HMAC-SHA256",
	Credential: "ak/20260608/cn-beijing/tos/request",
	Date:       "20260608T120000Z",
	Signature:  "sig",
})
```

Assert exact keys:

```go
want := map[string]string{
	"key":              "uploads/images/item/user_123/2026/06/img.png",
	"Content-Type":     "image/png",
	"policy":           "policy",
	"x-tos-algorithm":  "TOS4-HMAC-SHA256",
	"x-tos-credential": "ak/20260608/cn-beijing/tos/request",
	"x-tos-date":       "20260608T120000Z",
	"x-tos-signature":  "sig",
}
```

- [ ] **Step 2: Run adapter tests to verify they fail**

Run: `rtk go test ./internal/app/base/storage`

Expected: FAIL because the storage package does not exist.

- [ ] **Step 3: Add or confirm the TOS SDK dependency**

Run: `rtk go get github.com/volcengine/ve-tos-golang-sdk/v2@v2.7.17`

Expected: dependency is added or confirmed in `go.mod`. If the command fails with DNS/proxy errors, rerun with escalated permissions because dependency resolution requires network access.

- [ ] **Step 4: Implement the adapter**

Create `internal/app/base/storage/tos_signer.go`:

```go
package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	baseservice "github.com/zet-plane/live-auction-backend/internal/app/base/service"
	"github.com/zet-plane/live-auction-backend/config"
)

var ErrTOSDisabled = errors.New("tos storage disabled")
var ErrTOSConfig = errors.New("invalid tos storage config")

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
		Conditions: []tos.PostSignatureCondition{{
			Key:   "Content-Type",
			Value: input.ContentType,
		}},
		ContentLengthRange: &tos.ContentLengthRange{
			RangeStart: 1,
			RangeEnd:   input.Size,
		},
	})
	if err != nil {
		return baseservice.SignOutput{}, err
	}
	return baseservice.SignOutput{
		UploadURL:  s.uploadURL,
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
	}
}
```

- [ ] **Step 5: Verify and commit**

Run: `rtk go test ./internal/app/base/storage`

Expected: PASS.

Commit:

```bash
rtk git add go.mod go.sum internal/app/base/storage/tos_signer.go internal/app/base/storage/tos_signer_test.go
rtk git commit -m "feat(base): add tos post upload signer"
```

### Task 4: Wire HTTP Handler and Route

**Files:**
- Modify: `internal/app/base/handler/health.go`
- Modify: `internal/app/base/handler/health_test.go`
- Modify: `internal/app/base/router/router.go`
- Modify: `internal/app/base/init.go`

- [ ] **Step 1: Write handler/router tests**

Add tests to `internal/app/base/handler/health_test.go`:

```go
func TestSignImageUploadWithoutServiceReturnsInternal(t *testing.T)
func TestSignImageUploadBindingErrorReturnsBadRequest(t *testing.T)
```

The first test should reset upload service state, register:

```go
f.Post("/api/v1/base/uploads/images/sign", binding.JSON(dto.SignImageUploadRequest{}), SignImageUpload)
```

and send a valid JSON body. Expected status is 500 because the service is nil.

The binding test should send `{}` and expect 400 because required fields are missing.

Create `internal/app/base/router/router_test.go` for route-level auth protection:

```go
package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flamego/flamego"
)

func TestUploadSignRouteRequiresAuthorization(t *testing.T) {
	f := flamego.New()
	f.Use(flamego.Renderer())
	RegisterRoutes(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/base/uploads/images/sign", strings.NewReader(`{"filename":"a.png","content_type":"image/png","size":1,"usage":"item"}`))
	req.Header.Set("Content-Type", "application/json")
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run route tests to verify they fail**

Run: `rtk go test ./internal/app/base/...`

Expected: FAIL because the handler and route are missing.

- [ ] **Step 3: Wire handler init and endpoint**

In `internal/app/base/handler/health.go`, extend package state:

```go
uploadSvc *service.UploadService
```

Change `Init` to:

```go
func Init(d *gorm.DB, c *redis.Client, u *service.UploadService) {
	db = d
	cache = c
	uploadSvc = u
}
```

Add the handler:

```go
func SignImageUpload(r flamego.Render, req *http.Request, current *usermodel.User, body dto.SignImageUploadRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if uploadSvc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := uploadSvc.SignImageUpload(req.Context(), current, body.Input())
	if err != nil {
		logx.Warnw("SignImageUpload failed", "user_id", current.ID, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
```

Required imports are:

```go
import (
	"net/http"

	"github.com/flamego/binding"
	"github.com/zet-plane/live-auction-backend/internal/app/base/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/base/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)
```

Update existing health tests that call `Init(prevDB, prevCache)` to pass the previous upload service too.

- [ ] **Step 4: Register the route**

In `internal/app/base/router/router.go`, add imports:

```go
"github.com/flamego/binding"
"github.com/zet-plane/live-auction-backend/internal/app/base/dto"
userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
"github.com/zet-plane/live-auction-backend/internal/middleware/web"
```

Add inside `RegisterRoutes`:

```go
auth := web.Authorization(userhandler.AuthenticateToken)
f.Post("/api/v1/base/uploads/images/sign", auth, binding.JSON(dto.SignImageUploadRequest{}), handler.SignImageUpload)
```

- [ ] **Step 5: Construct the service in module load**

In `internal/app/base/init.go`, import storage and service packages. In `Load`:

```go
var signer service.Signer
tosSigner, err := storage.NewTOSPostSigner(engine.Config.Storage.TOS)
if err == nil {
	signer = tosSigner
}
uploadSvc := service.NewUploadService(signer, service.Options{
	PublicBaseURL: engine.Config.Storage.TOS.PublicBaseURL,
	MaxSizeBytes:  engine.Config.StorageTOSImageMaxSizeBytes(),
	Expires:       engine.Config.StorageTOSUploadExpires(),
})
handler.Init(engine.DB, engine.Cache, uploadSvc)
router.RegisterRoutes(engine.Flame)
```

If `NewTOSPostSigner` returns an error, log a warning without logging credentials:

```go
logx.Warnw("TOS upload signer disabled", "err", err)
```

This keeps health endpoints available when TOS is not configured; signing requests fail closed with `ErrInternal` because `signer` is nil.

- [ ] **Step 6: Verify and commit**

Run: `rtk go test ./internal/app/base/...`

Expected: PASS.

Commit:

```bash
rtk git add internal/app/base/handler/health.go internal/app/base/handler/health_test.go internal/app/base/router/router.go internal/app/base/init.go
rtk git commit -m "feat(base): expose image upload sign endpoint"
```

### Task 5: Final Verification

**Files:**
- Read: all files changed by Tasks 1-4

- [ ] **Step 1: Format changed Go files**

Run:

```bash
rtk gofmt -w config/vars.go config/config.go config/config_test.go internal/app/base/dto/upload.go internal/app/base/service/upload.go internal/app/base/service/upload_test.go internal/app/base/storage/tos_signer.go internal/app/base/storage/tos_signer_test.go internal/app/base/handler/health.go internal/app/base/handler/health_test.go internal/app/base/router/router.go internal/app/base/init.go
```

Expected: command exits 0.

- [ ] **Step 2: Run targeted tests**

Run:

```bash
rtk go test ./config ./internal/app/base/...
```

Expected: PASS for all listed packages.

- [ ] **Step 3: Run broad build**

Run:

```bash
rtk go test ./...
```

Expected: PASS. If unrelated existing tests fail, record the failing package and verify `./config ./internal/app/base/...` still passes.

- [ ] **Step 4: Inspect staged diff before final handoff**

Run:

```bash
rtk git status --short
rtk git log --oneline -5
```

Expected: only intended upload-signing commits are new; unrelated working-tree changes from before this work remain untouched.
