package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BaseURL    string
	BatchID    string
	UserCount  int
	OutputCSV  string
	InputCSV   string
	HTTPClient *http.Client
}

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type csvUser struct {
	Index    string
	Username string
	Password string
	UserID   string
	RoomID   string
	ItemID   string
}

type cleanupMerchantItem struct {
	ID     string `json:"id"`
	RoomID string `json:"room_id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help" {
		usage()
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch os.Args[1] {
	case "prepare":
		err = prepare(ctx, cfg)
	case "cleanup":
		err = cleanup(ctx, cfg)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Usage: pts-data prepare|cleanup")
	fmt.Println()
	fmt.Println("Required env:")
	fmt.Println("  PTS_BASE_URL       Public API base URL, for example https://<host>")
	fmt.Println()
	fmt.Println("Optional env:")
	fmt.Println("  PTS_BATCH_ID       Batch id, default agent_pts_auction_public_<timestamp>")
	fmt.Println("  PTS_USER_COUNT     Users to create, default 120")
	fmt.Println("  PTS_OUTPUT_CSV     Output CSV path, default run jmeter/users.csv")
	fmt.Println("  PTS_USERS_CSV      Cleanup CSV path, default PTS_OUTPUT_CSV")
	fmt.Println("  PTS_TIMEOUT        HTTP timeout, default 30s")
}

func loadConfig() (Config, error) {
	baseURL := strings.TrimRight(os.Getenv("PTS_BASE_URL"), "/")
	if baseURL == "" {
		return Config{}, errors.New("PTS_BASE_URL is required")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return Config{}, fmt.Errorf("invalid PTS_BASE_URL: %w", err)
	}

	batchID := os.Getenv("PTS_BATCH_ID")
	if batchID == "" {
		batchID = "agent_pts_auction_public_" + time.Now().Format("20060102150405")
	}

	outputCSV := getenv("PTS_OUTPUT_CSV", "docs/agent-testing/performance-runs/agent_pts_auction_public_20260606/jmeter/users.csv")
	inputCSV := getenv("PTS_USERS_CSV", outputCSV)
	timeout, err := durationEnv("PTS_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	return Config{
		BaseURL:   baseURL,
		BatchID:   batchID,
		UserCount: envInt("PTS_USER_COUNT", 120),
		OutputCSV: outputCSV,
		InputCSV:  inputCSV,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func prepare(ctx context.Context, cfg Config) error {
	password := batchPassword(cfg.BatchID)
	merchantToken, _, err := ensureAuth(ctx, cfg, merchantAccount(cfg.BatchID), password)
	if err != nil {
		return fmt.Errorf("merchant auth: %w", err)
	}
	if err := putJSON(ctx, cfg, "/api/v1/users/me", merchantToken, map[string]any{
		"name":     merchantDisplayName(cfg.BatchID),
		"identity": "merchant",
	}, nil); err != nil {
		return fmt.Errorf("promote merchant: %w", err)
	}

	var room struct {
		ID string `json:"id"`
	}
	if err := postJSON(ctx, cfg, "/api/v1/merchant/room", merchantToken, map[string]any{
		"title": "agent_pts_room_" + cfg.BatchID,
	}, &room); err != nil {
		return fmt.Errorf("activate room: %w", err)
	}
	if room.ID == "" {
		return errors.New("activate room returned empty id")
	}
	if err := postJSON(ctx, cfg, "/api/v1/rooms/"+url.PathEscape(room.ID)+"/start", merchantToken, nil, nil); err != nil {
		return fmt.Errorf("start room: %w", err)
	}

	now := time.Now().Add(-1 * time.Minute)
	end := time.Now().Add(2 * time.Hour)
	var item struct {
		ItemID string `json:"item_id"`
	}
	if err := postJSON(ctx, cfg, "/api/v1/items", merchantToken, map[string]any{
		"room_id":     room.ID,
		"title":       "agent_pts_item_" + cfg.BatchID,
		"description": "agent pts performance item",
		"image_url":   "https://example.com/agent-pts.png",
		"tags":        []string{"agent", "pts", "performance"},
		"rule": map[string]any{
			"start_price":   1000,
			"bid_increment": 100,
			"start_time":    now.Format(time.RFC3339),
			"end_time":      end.Format(time.RFC3339),
		},
	}, &item); err != nil {
		return fmt.Errorf("create item: %w", err)
	}
	if item.ItemID == "" {
		return errors.New("create item returned empty item_id")
	}
	if err := postJSON(ctx, cfg, "/api/v1/items/"+url.PathEscape(item.ItemID)+"/publish", merchantToken, nil, nil); err != nil {
		return fmt.Errorf("publish item: %w", err)
	}
	if err := postJSON(ctx, cfg, "/api/v1/items/"+url.PathEscape(item.ItemID)+"/start", merchantToken, nil, nil); err != nil {
		return fmt.Errorf("start item: %w", err)
	}

	users := make([]csvUser, 0, cfg.UserCount)
	for i := 0; i < cfg.UserCount; i++ {
		account := userAccount(cfg.BatchID, i)
		_, userID, err := ensureAuth(ctx, cfg, account, password)
		if err != nil {
			return fmt.Errorf("user auth index=%d: %w", i, err)
		}
		users = append(users, csvUser{
			Index:    strconv.Itoa(i),
			Username: account,
			Password: password,
			UserID:   userID,
			RoomID:   room.ID,
			ItemID:   item.ItemID,
		})
	}
	if err := writeUsersCSV(cfg.OutputCSV, users); err != nil {
		return err
	}

	fmt.Printf("PTS_DATA_PREPARED batch_id=%s users=%d output=%s room_created=yes item_created=yes\n", cfg.BatchID, len(users), cfg.OutputCSV)
	fmt.Println("Upload the generated file to PTS with node name users.csv and file name users.csv.")
	return nil
}

func cleanup(ctx context.Context, cfg Config) error {
	password := batchPassword(cfg.BatchID)
	merchantToken, _, merchantErr := login(ctx, cfg, merchantAccount(cfg.BatchID), password)

	cancelOK, cancelErr := 0, 0
	if merchantErr == nil {
		items, err := listBatchMerchantItems(ctx, cfg, merchantToken)
		if err == nil {
			for _, item := range items {
				if !isBatchMerchantItem(cfg.BatchID, item) {
					continue
				}
				if err := postJSON(ctx, cfg, "/api/v1/items/"+url.PathEscape(item.ID)+"/cancel", merchantToken, nil, nil); err != nil {
					cancelErr++
				} else {
					cancelOK++
				}
			}
		}
		var room struct {
			ID string `json:"id"`
		}
		if err := getJSON(ctx, cfg, "/api/v1/merchant/room", merchantToken, &room); err == nil && room.ID != "" {
			_ = postJSON(ctx, cfg, "/api/v1/rooms/"+url.PathEscape(room.ID)+"/end", merchantToken, nil, nil)
		}
	}

	users, err := readUsersCSV(cfg.InputCSV)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(users) == 0 {
		for i := 0; i < cfg.UserCount; i++ {
			users = append(users, csvUser{
				Username: userAccount(cfg.BatchID, i),
				Password: password,
			})
		}
	}

	loginOK, deleteOK, deleteErr := 0, 0, 0
	for _, user := range users {
		if user.Username == "" || user.Password == "" {
			continue
		}
		token, _, err := login(ctx, cfg, user.Username, user.Password)
		if err != nil {
			continue
		}
		loginOK++
		if err := deleteJSON(ctx, cfg, "/api/v1/users/me", token); err != nil {
			deleteErr++
		} else {
			deleteOK++
		}
	}

	merchantDelete := "skip"
	if merchantErr == nil {
		if err := deleteJSON(ctx, cfg, "/api/v1/users/me", merchantToken); err != nil {
			merchantDelete = "err"
		} else {
			merchantDelete = "ok"
		}
	}
	fmt.Printf("PTS_DATA_CLEANUP batch_id=%s users_seen=%d user_login_ok=%d user_delete_ok=%d user_delete_err=%d cancel_ok=%d cancel_err=%d delete_merchant=%s\n",
		cfg.BatchID, len(users), loginOK, deleteOK, deleteErr, cancelOK, cancelErr, merchantDelete)
	return nil
}

func ensureAuth(ctx context.Context, cfg Config, account string, password string) (string, string, error) {
	token, userID, registerErr := register(ctx, cfg, account, password)
	if registerErr == nil {
		return token, userID, nil
	}
	token, userID, loginErr := login(ctx, cfg, account, password)
	if loginErr == nil {
		return token, userID, nil
	}
	return "", "", fmt.Errorf("register failed: %v; login failed: %w", registerErr, loginErr)
}

func register(ctx context.Context, cfg Config, account string, password string) (string, string, error) {
	return auth(ctx, cfg, "/api/v1/auth/register", account, password)
}

func login(ctx context.Context, cfg Config, account string, password string) (string, string, error) {
	return auth(ctx, cfg, "/api/v1/auth/login", account, password)
}

func auth(ctx context.Context, cfg Config, path string, account string, password string) (string, string, error) {
	var result struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	err := postJSON(ctx, cfg, path, "", map[string]any{
		"account":  account,
		"password": password,
	}, &result)
	if err != nil {
		return "", "", err
	}
	if result.Token == "" || result.User.ID == "" {
		return "", "", errors.New("missing token or user id")
	}
	return result.Token, result.User.ID, nil
}

func listBatchMerchantItems(ctx context.Context, cfg Config, token string) ([]cleanupMerchantItem, error) {
	var result struct {
		List []cleanupMerchantItem `json:"list"`
	}
	path := "/api/v1/merchant/items?keyword=" + url.QueryEscape(cfg.BatchID) + "&page=1&page_size=100"
	if err := getJSON(ctx, cfg, path, token, &result); err != nil {
		return nil, err
	}
	return result.List, nil
}

func isBatchMerchantItem(batchID string, item cleanupMerchantItem) bool {
	return strings.Contains(item.Title, batchID) && strings.HasPrefix(item.Title, "agent_pts_item_")
}

func postJSON(ctx context.Context, cfg Config, path string, token string, body any, out any) error {
	return doJSON(ctx, cfg, http.MethodPost, path, token, body, out)
}

func getJSON(ctx context.Context, cfg Config, path string, token string, out any) error {
	return doJSON(ctx, cfg, http.MethodGet, path, token, nil, out)
}

func putJSON(ctx context.Context, cfg Config, path string, token string, body any, out any) error {
	return doJSON(ctx, cfg, http.MethodPut, path, token, body, out)
}

func deleteJSON(ctx context.Context, cfg Config, path string, token string) error {
	return doJSON(ctx, cfg, http.MethodDelete, path, token, nil, nil)
}

func doJSON(ctx context.Context, cfg Config, method string, path string, token string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Agent-Test-Batch", cfg.BatchID)

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var envelope apiResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("status=%d unparsed_response", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Code != 0 {
		return fmt.Errorf("status=%d code=%d msg=%s", resp.StatusCode, envelope.Code, envelope.Message)
	}
	if out != nil && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return err
		}
	}
	return nil
}

func writeUsersCSV(path string, users []csvUser) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"user_index", "username", "password", "user_id", "room_id", "item_id"}); err != nil {
		return err
	}
	for _, user := range users {
		if err := writer.Write([]string{user.Index, user.Username, user.Password, user.UserID, user.RoomID, user.ItemID}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func readUsersCSV(path string) ([]csvUser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	start := 0
	if strings.EqualFold(strings.Join(rows[0], ","), "user_index,username,password,user_id,room_id,item_id") {
		start = 1
	}
	users := make([]csvUser, 0, len(rows)-start)
	for _, row := range rows[start:] {
		if len(row) < 6 {
			continue
		}
		users = append(users, csvUser{
			Index:    row[0],
			Username: row[1],
			Password: row[2],
			UserID:   row[3],
			RoomID:   row[4],
			ItemID:   row[5],
		})
	}
	return users, nil
}

func batchPassword(batchID string) string {
	return "PerfPass_" + compactBatch(batchID)
}

func merchantAccount(batchID string) string {
	return compactBatch(batchID) + "_m"
}

func userAccount(batchID string, index int) string {
	return fmt.Sprintf("%s_u%03d", compactBatch(batchID), index)
}

func compactBatch(batchID string) string {
	replacer := strings.NewReplacer("agent_", "a_", "perf_", "p_", "auction_", "auc_", "public_", "pub_")
	value := replacer.Replace(batchID)
	if len(value) > 40 {
		value = value[len(value)-40:]
	}
	return value
}

func merchantDisplayName(batchID string) string {
	name := "agent pts merchant " + compactBatch(batchID)
	if len(name) > 64 {
		return name[:64]
	}
	return name
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err == nil {
		return value, nil
	}
	seconds, atoiErr := strconv.Atoi(raw)
	if atoiErr != nil {
		return 0, fmt.Errorf("%s must be a duration like 30s or integer seconds", key)
	}
	return time.Duration(seconds) * time.Second, nil
}
