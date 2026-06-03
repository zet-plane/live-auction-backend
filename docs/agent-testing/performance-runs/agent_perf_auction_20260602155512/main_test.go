package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestPriceTooLowIsExpectedRejectAndDoesNotTriggerSystemStop(t *testing.T) {
	summary := StageSummary{
		Total:                   100,
		ExpectedBusinessRejects: 10,
		BusinessCodes:           map[string]int64{"0": 90, "40003": 10},
	}

	if reason := thresholdStopReason(summary); reason != "" {
		t.Fatalf("expected price_too_low rejects to be excluded from stop threshold, got %q", reason)
	}
}

func TestBuildBidRequestReadsCurrentPriceBeforeIncrement(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/items/item_test" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		raw, _ := json.Marshal(apiResponse{
			Code:    0,
			Message: "ok",
			Data: mustRawMessage(t, map[string]any{
				"current_price": float64(1500),
				"rule": map[string]any{
					"bid_increment": float64(100),
				},
			}),
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(raw)),
			Header:     make(http.Header),
		}, nil
	})}

	cfg := Config{BatchID: "batch_test", BaseURL: "http://example.test"}
	data := &TestData{
		ItemID:     "item_test",
		UserTokens: []string{"token_1"},
		HTTPClient: client,
	}

	spec, err := buildBidRequest(context.Background(), cfg, data, StageConfig{Name: "stage_test"}, 0, 42)
	if err != nil {
		t.Fatalf("buildBidRequest returned error: %v", err)
	}

	var body struct {
		Price int64 `json:"price"`
	}
	if err := json.Unmarshal(spec.Body, &body); err != nil {
		t.Fatalf("unmarshal bid body: %v", err)
	}
	if body.Price != 1600 {
		t.Fatalf("expected bid price 1600, got %d", body.Price)
	}
}

func TestLoadConfigFiltersStagesFromStartQPS(t *testing.T) {
	t.Setenv("PERF_START_QPS", "40")

	cfg := loadConfig()

	if len(cfg.Stages) != 3 {
		t.Fatalf("expected 3 stages from 40 QPS onward, got %d", len(cfg.Stages))
	}
	if cfg.Stages[0].TargetQPS != 40 {
		t.Fatalf("expected first stage to start at 40 QPS, got %.2f", cfg.Stages[0].TargetQPS)
	}
}

func TestDefaultStagesReachOneWebSocketPerUserAtPeak(t *testing.T) {
	cfg := loadConfig()

	if maxTargetWS(cfg.Stages) != cfg.UserCount {
		t.Fatalf("expected peak WebSocket target to match user count; got ws=%d users=%d", maxTargetWS(cfg.Stages), cfg.UserCount)
	}
}

func mustRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw message: %v", err)
	}
	return raw
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
