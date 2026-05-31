// Agent testing Go runner template.
// Copy this file to /tmp/agent-runner-<batch>/main.go, create go.mod (see guides/go-runner.md),
// fill CONFIG and SCENARIOS, then: go run main.go
// This file never enters the repository at runtime.

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

// ── CONFIG (agent fills) ─────────────────────────────────────────────────────

const (
	baseURL   = "http://127.0.0.1:8080"
	batchID   = "agent_item_20260524120000" // REQUIRED: replace module name and timestamp before running
	redisAddr = "127.0.0.1:6379"
)

// DSN is read from TEST_DSN env var — do not write credentials into this file.
// Example: TEST_DSN="user:pass@tcp(127.0.0.1:3306)/live_auction?parseTime=true" go run main.go

// ── TYPES ────────────────────────────────────────────────────────────────────

// Result holds the structured evidence for one test scenario.
type Result struct {
	Name     string
	Pass     bool
	Reason   string
	Request  string // "METHOD /path {body summary}"
	Response string // "status key=val key=val"
	DB       string // "SELECT ... -> [{col:val}]" or "N/A"
	Redis    string // "key -> val" or "N/A"
}

// ── SCENARIOS (agent fills) ──────────────────────────────────────────────────
// Declare package-level vars here to share state between sequential scenarios.
// Example:
//
//	var (
//	    merchantToken string
//	    createdItemID string
//	)

func scenarios() []func() Result {
	return []func() Result{
		// TODO: add scenario functions following the module contract.
		// Example:
		//   caseCreateItem,
		//   casePublishItem,
	}
}

// Example scenario — delete or replace when filling in real cases:
//
// func caseCreateItem() Result {
//     status, body := httpDo("POST", "/api/v1/items", map[string]any{
//         "title":       batchID + "_jade",
//         "room_id":     "room_test_001",
//         "description": "agent test item",
//         "rule": map[string]any{
//             "start_price":   100,
//             "bid_increment": 10,
//             "start_time":    "2026-06-01T10:00:00Z",
//             "end_time":      "2026-06-01T11:00:00Z",
//         },
//     }, merchantToken)
//     itemID := mustStr(body, "data", "item_id")
//     createdItemID = itemID
//     rows := dbRows("SELECT id, status FROM auction_items WHERE id = ? AND deleted_at IS NULL", itemID)
//     pass := status == 200 && itemID != "" && len(rows) == 1 && rows[0]["status"] == "draft"
//     return Result{
//         Name:     "create item",
//         Pass:     pass,
//         Reason:   fmt.Sprintf("status=%d item_id=%s db_status=%s", status, itemID, safeGet(rows, 0, "status")),
//         Request:  "POST /api/v1/items {title:" + batchID + "_jade}",
//         Response: fmt.Sprintf("%d item_id=%s", status, itemID),
//         DB:       fmt.Sprintf("SELECT id,status WHERE id=%s -> %v", itemID, rows),
//         Redis:    "N/A",
//     }
// }

// ── CLEANUP (agent fills) ────────────────────────────────────────────────────
// Only clean up data created in this batch (rows with batchID prefix, known IDs).

func cleanup() {
	fmt.Println("\n=== CLEANUP")
	// TODO: agent adds cleanup actions here. Examples:
	//
	// Soft-delete items created in this batch:
	//   res, err := db.ExecContext(ctx, "UPDATE auction_items SET deleted_at=NOW() WHERE title LIKE ? AND deleted_at IS NULL", batchID+"%")
	//   n, _ := res.RowsAffected()
	//   fmt.Printf("  soft-delete auction_items LIKE %s%%: rows=%d err=%v\n", batchID, n, err)
	//
	// Delete Redis keys created in this batch:
	//   rdb.Del(ctx, "auction:item:"+createdItemID+":state")
	//   fmt.Printf("  del auction:item:%s:state\n", createdItemID)
	fmt.Println("  (no cleanup configured)")
}

// ── HELPERS (do not modify) ──────────────────────────────────────────────────

var (
	httpClient = &http.Client{Timeout: 10 * time.Second}
	db         *sql.DB
	rdb        *redis.Client
	ctx        = context.Background()
)

func init() {
	if dsn := os.Getenv("TEST_DSN"); dsn != "" {
		var err error
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "db open: %v\n", err)
		} else if err = db.Ping(); err != nil {
			fmt.Fprintf(os.Stderr, "db ping: %v\n", err)
			db = nil
		}
	}
	rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
}

// httpDo sends an HTTP request and returns the status code and parsed JSON body.
// Pass token="" to omit the Authorization header.
func httpDo(method, path string, body any, token string) (int, map[string]any) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, baseURL+path, r)
	if err != nil {
		return 0, map[string]any{"error": err.Error()}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, map[string]any{"error": err.Error()}
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// dbRows executes a SELECT and returns rows as []map[string]string.
// Returns nil if TEST_DSN was not set.
func dbRows(query string, args ...any) []map[string]string {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return []map[string]string{{"error": err.Error()}}
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []map[string]string
	for rows.Next() {
		vals := make([][]byte, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...) //nolint:errcheck
		row := make(map[string]string, len(cols))
		for i, col := range cols {
			row[col] = string(vals[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		out = append(out, map[string]string{"error": err.Error()})
	}
	return out
}

// redisGet returns the string value of a key, or "" if missing or on error.
func redisGet(key string) string {
	val, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return ""
	}
	return val
}

// redisHGetAll returns all fields of a hash key, or nil on error.
func redisHGetAll(key string) map[string]string {
	val, err := rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil
	}
	return val
}

// redisZMembers returns all members of a sorted set (score ascending), or nil on error.
func redisZMembers(key string) []string {
	vals, err := rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil
	}
	return vals
}

// runConcurrent launches n goroutines each calling fn and collects all results.
func runConcurrent(n int, fn func() Result) []Result {
	out := make([]Result, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			out[idx] = fn()
		}(i)
	}
	wg.Wait()
	return out
}

// mustStr extracts a string from a nested map path.
// mustStr(body, "data", "item_id") reads body["data"]["item_id"].
func mustStr(m map[string]any, keys ...string) string {
	cur := m
	for i, k := range keys {
		if i == len(keys)-1 {
			s, _ := cur[k].(string)
			return s
		}
		next, _ := cur[k].(map[string]any)
		if next == nil {
			return ""
		}
		cur = next
	}
	return ""
}

// safeGet returns rows[idx][key] or "" if idx is out of bounds.
func safeGet(rows []map[string]string, idx int, key string) string {
	if idx >= len(rows) {
		return ""
	}
	return rows[idx][key]
}

// jsonStr serialises v to compact JSON for use in Result fields.
func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// filterLines returns lines from s that contain substr, joined by newline.
// Useful for extracting relevant lines from server log output piped into a Result field.
func filterLines(s, substr string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// ── MAIN (do not modify) ─────────────────────────────────────────────────────

func main() {
	defer cleanup()

	fns := scenarios()
	if len(fns) == 0 {
		fmt.Println("no scenarios defined — fill the scenarios() function and try again")
		return
	}

	var results []Result
	for _, fn := range fns {
		r := fn()
		results = append(results, r)
		printResult(r)
	}

	pass, fail := 0, 0
	for _, r := range results {
		if r.Pass {
			pass++
		} else {
			fail++
		}
	}
	fmt.Printf("\n=== SUMMARY\n  PASS: %d  FAIL: %d\n  BATCH_ID: %s\n", pass, fail, batchID)
}

func printResult(r Result) {
	status := "PASS"
	if !r.Pass {
		status = "FAIL"
	}
	fmt.Printf("\n=== CASE: %s\n", r.Name)
	fmt.Printf("  REQUEST:  %s\n", r.Request)
	fmt.Printf("  RESPONSE: %s\n", r.Response)
	fmt.Printf("  DB:       %s\n", r.DB)
	fmt.Printf("  REDIS:    %s\n", r.Redis)
	fmt.Printf("  RESULT:   %s — %s\n", status, r.Reason)
}
