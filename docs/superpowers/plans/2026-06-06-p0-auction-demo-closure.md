# P0 Auction Demo Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the P0 auction demo path by adding automatic auction start, deposit refund/forfeiture, order settlement hooks, and updated E2E contract documentation.

**Architecture:** Keep the existing module boundaries. `item.Service` owns auction start and auction settlement, `deposit.Service` owns terminal deposit state transitions, and `order.Service` calls deposit settlement hooks after committed order transitions. The implementation is local-unit-test-first; dependency-backed E2E execution remains approval-gated.

**Tech Stack:** Go, GORM, Redis-backed item cache, robfig cron through `kernel.Engine.Cron`, Flamego handlers, project fake-store unit tests, `docs/agent-testing` contracts.

---

## File Structure

- Modify `internal/app/deposit/model/deposit.go`: keep existing model; no schema field added.
- Modify `internal/app/deposit/dao/deposit.go`: add list and conditional transition methods for paid deposits.
- Modify `internal/app/deposit/service/service.go`: add `SettlementSummary`, `RefundNonWinners`, `RefundWinner`, and `ForfeitWinner`.
- Modify `internal/app/deposit/service/service_test.go`: add fake-store methods and RED/GREEN unit tests for terminal deposit settlement.
- Modify `internal/app/deposit/init.go`: after creating `deposit.Svc`, inject it into `order.Svc` with `SetDepositSettler`.
- Modify `internal/app/order/service/service.go`: add `DepositSettler`, setter, and settlement calls after payment/cancellation status commits.
- Modify `internal/app/order/service/cron.go`: forfeit winner deposits after pending orders expire.
- Modify `internal/app/order/service/service_test.go`: add fake settler and tests for pay/cancel/expiry hooks.
- Modify `internal/app/item/dao/item.go`: add `ListPublishedItemsPastStartTime`.
- Modify `internal/app/item/service/service.go`: add `StartDueAuctions`, shared start helper, and non-winner refund after settlement.
- Modify `internal/app/item/service/bid_service.go`: refund non-winners in price-cap settlement.
- Modify `internal/app/item/service/service_test.go`: add fake-store and fake-deposit support plus automatic start/refund tests.
- Modify `internal/app/item/service/bid_service_test.go`: verify price-cap settlement calls non-winner refund.
- Modify `internal/app/item/init.go`: register `item.start_due_auctions` cron.
- Modify `docs/agent-testing/flows/auction-lifecycle.md`: include order payment and deposit terminal-state assertions.
- Modify `docs/agent-testing/modules/deposit.md`: document settlement methods and terminal timestamp interpretation.
- Modify `docs/agent-testing/modules/order.md`: document deposit hooks on paid/cancelled/expired.
- Modify `docs/agent-testing/modules/payment.md`: document payment-triggered winner refund.
- Modify `docs/todo.md`: update P0 checkboxes only after implementation tests pass; E2E execution remains not complete unless an approved E2E run is actually performed.

## Task 1: Deposit Settlement Service

**Files:**
- Modify `internal/app/deposit/dao/deposit.go`
- Modify `internal/app/deposit/service/service.go`
- Modify `internal/app/deposit/service/service_test.go`

- [ ] **Step 1: Write failing deposit settlement tests**

Append these tests to `internal/app/deposit/service/service_test.go`, and add the fake-store methods shown below before the tests.

```go
func (s *fakeStore) ListPaidDepositsByItem(itemID string) ([]model.Deposit, error) {
	var result []model.Deposit
	for _, d := range s.deposits {
		if d.ItemID == itemID && d.Status == model.DepositPaid {
			result = append(result, *d)
		}
	}
	return result, nil
}

func (s *fakeStore) TransitionDepositStatus(itemID, userID string, from, to model.DepositStatus, terminalAt *time.Time) (bool, error) {
	d, ok := s.deposits[depositKey(itemID, userID)]
	if !ok || d.Status != from {
		return false, nil
	}
	d.Status = to
	d.RefundedAt = terminalAt
	return true, nil
}

func TestRefundNonWinnersRefundsPaidDepositsExceptWinner(t *testing.T) {
	store := newFakeStore()
	paidAt := store.now.Add(-time.Minute)
	store.deposits[depositKey("item_1", "user_a")] = &model.Deposit{ID: "deposit_a", ItemID: "item_1", UserID: "user_a", Amount: 5000, Status: model.DepositPaid, PaidAt: &paidAt}
	store.deposits[depositKey("item_1", "user_b")] = &model.Deposit{ID: "deposit_b", ItemID: "item_1", UserID: "user_b", Amount: 5000, Status: model.DepositPaid, PaidAt: &paidAt}
	store.deposits[depositKey("item_1", "user_c")] = &model.Deposit{ID: "deposit_c", ItemID: "item_1", UserID: "user_c", Amount: 5000, Status: model.DepositRefunded, PaidAt: &paidAt}
	svc := NewService(store)
	svc.now = func() time.Time { return store.now }

	summary, err := svc.RefundNonWinners(context.Background(), "item_1", "user_b")

	if err != nil {
		t.Fatalf("RefundNonWinners returned error: %v", err)
	}
	if summary.Refunded != 1 || summary.Forfeited != 0 || summary.Skipped != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if got := store.deposits[depositKey("item_1", "user_a")]; got.Status != model.DepositRefunded || got.RefundedAt == nil {
		t.Fatalf("expected user_a refunded with terminal time, got %+v", got)
	}
	if got := store.deposits[depositKey("item_1", "user_b")]; got.Status != model.DepositPaid {
		t.Fatalf("expected winner to remain paid, got %+v", got)
	}
}

func TestRefundWinnerAndForfeitWinnerAreIdempotent(t *testing.T) {
	store := newFakeStore()
	paidAt := store.now.Add(-time.Minute)
	store.deposits[depositKey("item_1", "winner_paid")] = &model.Deposit{ID: "deposit_w", ItemID: "item_1", UserID: "winner_paid", Amount: 5000, Status: model.DepositPaid, PaidAt: &paidAt}
	store.deposits[depositKey("item_1", "winner_done")] = &model.Deposit{ID: "deposit_done", ItemID: "item_1", UserID: "winner_done", Amount: 5000, Status: model.DepositRefunded, PaidAt: &paidAt}
	svc := NewService(store)
	svc.now = func() time.Time { return store.now }

	refundSummary, err := svc.RefundWinner(context.Background(), "item_1", "winner_paid")
	if err != nil {
		t.Fatalf("RefundWinner returned error: %v", err)
	}
	if refundSummary.Refunded != 1 {
		t.Fatalf("expected one refund, got %+v", refundSummary)
	}
	if got := store.deposits[depositKey("item_1", "winner_paid")]; got.Status != model.DepositRefunded || got.RefundedAt == nil {
		t.Fatalf("expected winner_paid refunded, got %+v", got)
	}

	second, err := svc.RefundWinner(context.Background(), "item_1", "winner_done")
	if err != nil {
		t.Fatalf("second RefundWinner returned error: %v", err)
	}
	if second.Refunded != 0 || second.Skipped != 1 {
		t.Fatalf("expected terminal deposit skipped, got %+v", second)
	}

	store.deposits[depositKey("item_2", "winner_default")] = &model.Deposit{ID: "deposit_f", ItemID: "item_2", UserID: "winner_default", Amount: 5000, Status: model.DepositPaid, PaidAt: &paidAt}
	forfeitSummary, err := svc.ForfeitWinner(context.Background(), "item_2", "winner_default")
	if err != nil {
		t.Fatalf("ForfeitWinner returned error: %v", err)
	}
	if forfeitSummary.Forfeited != 1 {
		t.Fatalf("expected one forfeiture, got %+v", forfeitSummary)
	}
	if got := store.deposits[depositKey("item_2", "winner_default")]; got.Status != model.DepositForfeited || got.RefundedAt == nil {
		t.Fatalf("expected winner_default forfeited, got %+v", got)
	}
}
```

- [ ] **Step 2: Run RED deposit tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/deposit/service -run 'TestRefundNonWinners|TestRefundWinnerAndForfeitWinner' -count=1
```

Expected: FAIL because `RefundNonWinners`, `RefundWinner`, `ForfeitWinner`, and the new DAO methods are undefined.

- [ ] **Step 3: Implement deposit DAO methods**

In `internal/app/deposit/dao/deposit.go`, extend `Store`:

```go
	ListPaidDepositsByItem(itemID string) ([]model.Deposit, error)
	TransitionDepositStatus(itemID, userID string, from, to model.DepositStatus, terminalAt *time.Time) (bool, error)
```

Add `time` to imports and implement:

```go
func (s *GormStore) ListPaidDepositsByItem(itemID string) ([]model.Deposit, error) {
	var deposits []model.Deposit
	err := s.db.Where("item_id = ? AND status = ?", itemID, model.DepositPaid).Find(&deposits).Error
	return deposits, err
}

func (s *GormStore) TransitionDepositStatus(itemID, userID string, from, to model.DepositStatus, terminalAt *time.Time) (bool, error) {
	updates := map[string]any{"status": to}
	if terminalAt != nil {
		updates["refunded_at"] = terminalAt
	}
	result := s.db.Model(&model.Deposit{}).
		Where("item_id = ? AND user_id = ? AND status = ?", itemID, userID, from).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
```

- [ ] **Step 4: Implement deposit service methods**

In `internal/app/deposit/service/service.go`, add:

```go
type SettlementSummary struct {
	Refunded  int
	Forfeited int
	Skipped   int
}
```

Then add:

```go
func (s *Service) RefundNonWinners(ctx context.Context, itemID, winnerUserID string) (summary SettlementSummary, err error) {
	itemID = strings.TrimSpace(itemID)
	winnerUserID = strings.TrimSpace(winnerUserID)
	finish := observability.Track(ctx, "deposit.refund_non_winners", "item_id", itemID, "winner_user_id", winnerUserID)
	defer func() {
		finish(&err, "refunded", summary.Refunded, "skipped", summary.Skipped)
	}()
	if itemID == "" {
		return summary, errorx.ErrInvalidRequest
	}
	deposits, err := s.store.ListPaidDepositsByItem(itemID)
	if err != nil {
		return summary, err
	}
	now := s.now()
	for _, deposit := range deposits {
		if deposit.UserID == winnerUserID {
			summary.Skipped++
			continue
		}
		ok, err := s.store.TransitionDepositStatus(itemID, deposit.UserID, model.DepositPaid, model.DepositRefunded, &now)
		if err != nil {
			return summary, err
		}
		if ok {
			summary.Refunded++
		} else {
			summary.Skipped++
		}
	}
	return summary, nil
}

func (s *Service) RefundWinner(ctx context.Context, itemID, userID string) (SettlementSummary, error) {
	return s.settleWinnerDeposit(ctx, "deposit.refund_winner", itemID, userID, model.DepositRefunded)
}

func (s *Service) ForfeitWinner(ctx context.Context, itemID, userID string) (SettlementSummary, error) {
	return s.settleWinnerDeposit(ctx, "deposit.forfeit_winner", itemID, userID, model.DepositForfeited)
}

func (s *Service) settleWinnerDeposit(ctx context.Context, operation, itemID, userID string, target model.DepositStatus) (summary SettlementSummary, err error) {
	itemID = strings.TrimSpace(itemID)
	userID = strings.TrimSpace(userID)
	finish := observability.Track(ctx, operation, "item_id", itemID, "user_id", userID, "target_status", string(target))
	defer func() {
		finish(&err, "refunded", summary.Refunded, "forfeited", summary.Forfeited, "skipped", summary.Skipped)
	}()
	if itemID == "" || userID == "" {
		return summary, errorx.ErrInvalidRequest
	}
	now := s.now()
	ok, err := s.store.TransitionDepositStatus(itemID, userID, model.DepositPaid, target, &now)
	if err != nil {
		return summary, err
	}
	if !ok {
		summary.Skipped = 1
		return summary, nil
	}
	if target == model.DepositRefunded {
		summary.Refunded = 1
	} else if target == model.DepositForfeited {
		summary.Forfeited = 1
	}
	return summary, nil
}
```

- [ ] **Step 5: Run GREEN deposit tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/deposit/service -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit deposit settlement**

Run:

```bash
rtk git add internal/app/deposit/dao/deposit.go internal/app/deposit/service/service.go internal/app/deposit/service/service_test.go
rtk git commit -m "feat: settle auction deposits"
```

## Task 2: Order Deposit Hooks

**Files:**
- Modify `internal/app/order/service/service.go`
- Modify `internal/app/order/service/cron.go`
- Modify `internal/app/order/service/service_test.go`
- Modify `internal/app/deposit/init.go`

- [ ] **Step 1: Write failing order hook tests**

In `internal/app/order/service/service_test.go`, add this fake settler after `fakeStore`:

```go
type fakeDepositSettler struct {
	refunded []string
	forfeited []string
	refundErr error
	forfeitErr error
}

func (s *fakeDepositSettler) RefundWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error) {
	s.refunded = append(s.refunded, itemID+"\x00"+userID)
	return depositservice.SettlementSummary{Refunded: 1}, s.refundErr
}

func (s *fakeDepositSettler) ForfeitWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error) {
	s.forfeited = append(s.forfeited, itemID+"\x00"+userID)
	return depositservice.SettlementSummary{Forfeited: 1}, s.forfeitErr
}
```

Add import:

```go
depositservice "github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
```

Append tests:

```go
func TestPayRefundsWinnerDepositAfterStatusCommit(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	settler := &fakeDepositSettler{}
	svc.SetDepositSettler(settler)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

	err := svc.Pay(context.Background(), &usermodel.User{ID: "user_1"}, order.ID)

	if err != nil {
		t.Fatalf("Pay returned error: %v", err)
	}
	if len(settler.refunded) != 1 || settler.refunded[0] != "item_1\x00user_1" {
		t.Fatalf("expected winner deposit refunded, got %#v", settler.refunded)
	}
}

func TestCancelForfeitsWinnerDepositAfterStatusCommit(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	settler := &fakeDepositSettler{}
	svc.SetDepositSettler(settler)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

	err := svc.Cancel(context.Background(), &usermodel.User{ID: "user_1"}, order.ID)

	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if len(settler.forfeited) != 1 || settler.forfeited[0] != "item_1\x00user_1" {
		t.Fatalf("expected winner deposit forfeited, got %#v", settler.forfeited)
	}
}

func TestScanExpiredOrdersForfeitsWinnerDeposit(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	settler := &fakeDepositSettler{}
	svc.SetDepositSettler(settler)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)
	store.orders[order.ID].ExpiredAt = svc.now().Add(-time.Minute)

	svc.ScanExpiredOrders(context.Background())

	if len(settler.forfeited) != 1 || settler.forfeited[0] != "item_1\x00user_1" {
		t.Fatalf("expected expired order to forfeit deposit, got %#v", settler.forfeited)
	}
}
```

- [ ] **Step 2: Run RED order tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/order/service -run 'TestPayRefundsWinnerDeposit|TestCancelForfeitsWinnerDeposit|TestScanExpiredOrdersForfeitsWinnerDeposit' -count=1
```

Expected: FAIL because `SetDepositSettler` and `DepositSettler` are undefined.

- [ ] **Step 3: Add order service dependency and setter**

In `internal/app/order/service/service.go`, import:

```go
depositservice "github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
"github.com/zet-plane/live-auction-backend/pkg/logx"
```

Add to the service file:

```go
type DepositSettler interface {
	RefundWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error)
	ForfeitWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error)
}
```

Add field:

```go
	depositSettler DepositSettler
```

Add setter:

```go
func (s *Service) SetDepositSettler(settler DepositSettler) {
	s.depositSettler = settler
}
```

- [ ] **Step 4: Hook Pay and Cancel**

In `Pay`, after the successful `UpdateOrderStatus` block and before `return nil`, add:

```go
	if s.depositSettler != nil {
		if _, settleErr := s.depositSettler.RefundWinner(ctx, order.ItemID, order.UserID); settleErr != nil {
			logx.Warnw("order.Pay refund winner deposit failed", "order_id", order.ID, "item_id", order.ItemID, "user_id", order.UserID, "err", settleErr)
		}
	}
```

In `Cancel`, after `ok` is confirmed true and before `return nil`, add:

```go
	if s.depositSettler != nil {
		if _, settleErr := s.depositSettler.ForfeitWinner(ctx, order.ItemID, order.UserID); settleErr != nil {
			logx.Warnw("order.Cancel forfeit winner deposit failed", "order_id", order.ID, "item_id", order.ItemID, "user_id", order.UserID, "err", settleErr)
		}
	}
```

- [ ] **Step 5: Hook expired-order scan**

In `internal/app/order/service/cron.go`, after a pending order successfully changes to expired, add:

```go
		ok, err := s.store.UpdateOrderStatus(o.ID, model.OrderPending, model.OrderExpired)
		if err != nil {
			logx.Errorf("[order] ScanExpiredOrders update %s error: %v", o.ID, err)
			continue
		}
		if !ok {
			continue
		}
		if s.depositSettler != nil {
			if _, settleErr := s.depositSettler.ForfeitWinner(ctx, o.ItemID, o.UserID); settleErr != nil {
				logx.Warnw("[order] ScanExpiredOrders deposit forfeit failed", "order_id", o.ID, "item_id", o.ItemID, "user_id", o.UserID, "err", settleErr)
			}
		}
		updatedCount++
```

This replaces the current loop body that increments `updatedCount` even when the conditional update returns `ok=false`.

- [ ] **Step 6: Wire deposit into order after deposit loads**

In `internal/app/deposit/init.go`, import:

```go
orderapp "github.com/zet-plane/live-auction-backend/internal/app/order"
```

After `Svc = service.NewService(store)`, add:

```go
	if orderapp.Svc != nil {
		orderapp.Svc.SetDepositSettler(Svc)
	}
```

- [ ] **Step 7: Run GREEN order tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/order/service ./internal/app/deposit -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit order hooks**

Run:

```bash
rtk git add internal/app/order/service/service.go internal/app/order/service/cron.go internal/app/order/service/service_test.go internal/app/deposit/init.go
rtk git commit -m "feat: settle deposits from order transitions"
```

## Task 3: Item Automatic Start and Auction Non-Winner Refunds

**Files:**
- Modify `internal/app/item/dao/item.go`
- Modify `internal/app/item/service/service.go`
- Modify `internal/app/item/service/bid_service.go`
- Modify `internal/app/item/service/service_test.go`
- Modify `internal/app/item/service/bid_service_test.go`
- Modify `internal/app/item/init.go`

- [ ] **Step 1: Write failing automatic-start tests**

In `internal/app/item/service/service_test.go`, add this fake-store method near `ListOngoingItemsPastEndTime`:

```go
func (s *fakeStore) ListPublishedItemsPastStartTime(before time.Time, limit int) ([]itemmodel.ItemWithRule, error) {
	var result []itemmodel.ItemWithRule
	for _, item := range s.items {
		if item.Status != itemmodel.ItemPublished {
			continue
		}
		rule := s.rules[item.RuleID]
		if rule == nil || rule.StartTime.After(before) {
			continue
		}
		itemCopy := *item
		ruleCopy := *rule
		result = append(result, itemmodel.ItemWithRule{Item: &itemCopy, Rule: &ruleCopy})
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}
```

Append tests:

```go
func TestStartDueAuctionsStartsPublishedItemsPastStartTime(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	svc := NewService(store, itemdto.DefaultAuctionPolicy(), cache, nil, nil, nil)
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	dueItem := &itemmodel.AuctionItem{ID: "item_due", MerchantID: "merchant_1", RoomID: "room_1", RuleID: "rule_due", Status: itemmodel.ItemPublished}
	dueRule := &itemmodel.AuctionRule{ID: "rule_due", ItemID: "item_due", StartPrice: 1000, BidIncrement: 100, StartTime: now.Add(-time.Second), EndTime: now.Add(time.Minute)}
	futureItem := &itemmodel.AuctionItem{ID: "item_future", MerchantID: "merchant_1", RoomID: "room_1", RuleID: "rule_future", Status: itemmodel.ItemPublished}
	futureRule := &itemmodel.AuctionRule{ID: "rule_future", ItemID: "item_future", StartPrice: 1000, BidIncrement: 100, StartTime: now.Add(time.Minute), EndTime: now.Add(2 * time.Minute)}
	if err := store.CreateItemWithRule(dueItem, dueRule); err != nil {
		t.Fatalf("CreateItemWithRule due: %v", err)
	}
	if err := store.CreateItemWithRule(futureItem, futureRule); err != nil {
		t.Fatalf("CreateItemWithRule future: %v", err)
	}

	svc.StartDueAuctions(context.Background())

	if store.items["item_due"].Status != itemmodel.ItemOngoing {
		t.Fatalf("expected due item ongoing, got %s", store.items["item_due"].Status)
	}
	if store.items["item_future"].Status != itemmodel.ItemPublished {
		t.Fatalf("expected future item still published, got %s", store.items["item_future"].Status)
	}
	if _, ok := cache.states["item_due"]; !ok {
		t.Fatal("expected due item auction state initialized")
	}
	if store.roomCurrentItems["room_1"] != "item_due" {
		t.Fatalf("expected room current item to be item_due, got %q", store.roomCurrentItems["room_1"])
	}
}
```

- [ ] **Step 2: Run RED automatic-start test**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run TestStartDueAuctionsStartsPublishedItemsPastStartTime -count=1
```

Expected: FAIL because `StartDueAuctions` and `ListPublishedItemsPastStartTime` are undefined.

- [ ] **Step 3: Implement published-item DAO scan**

In `internal/app/item/dao/item.go`, extend `Store`:

```go
	ListPublishedItemsPastStartTime(before time.Time, limit int) ([]model.ItemWithRule, error)
```

Implement:

```go
func (s *GormStore) ListPublishedItemsPastStartTime(before time.Time, limit int) ([]model.ItemWithRule, error) {
	var items []model.AuctionItem
	if err := s.db.Model(&model.AuctionItem{}).
		Joins("JOIN auction_rules ON auction_rules.id = auction_items.rule_id").
		Where("auction_items.status = ? AND auction_items.deleted_at IS NULL AND auction_rules.start_time <= ?", model.ItemPublished, before).
		Order("auction_rules.start_time ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []model.ItemWithRule{}, nil
	}
	ruleIDs := make([]string, 0, len(items))
	for _, item := range items {
		ruleIDs = append(ruleIDs, item.RuleID)
	}
	var rules []model.AuctionRule
	if err := s.db.Where("id IN ?", ruleIDs).Find(&rules).Error; err != nil {
		return nil, err
	}
	ruleByID := make(map[string]*model.AuctionRule, len(rules))
	for i := range rules {
		ruleByID[rules[i].ID] = &rules[i]
	}
	result := make([]model.ItemWithRule, 0, len(items))
	for i := range items {
		rule := ruleByID[items[i].RuleID]
		if rule == nil {
			continue
		}
		itemCopy := items[i]
		result = append(result, model.ItemWithRule{Item: &itemCopy, Rule: rule})
	}
	return result, nil
}
```

- [ ] **Step 4: Extract shared start helper and add StartDueAuctions**

In `internal/app/item/service/service.go`, change `StartItem` to authorize then call a helper:

```go
func (s *Service) StartItem(ctx context.Context, current *usermodel.User, itemID string) (err error) {
	defer observability.Track(ctx, "item.start", "merchant_id", userID(current), "item_id", strings.TrimSpace(itemID))(&err)

	item, rule, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	return s.startItemWithRule(ctx, item, rule)
}
```

Create helper by moving the existing body after `findMerchantItem` into:

```go
func (s *Service) startItemWithRule(ctx context.Context, item *model.AuctionItem, rule *model.AuctionRule) error {
	if item.Status != model.ItemPublished {
		return errorx.ErrInvalidRequest
	}
	if s.cache != nil {
		state := itemcache.AuctionState{
			RoomID:            item.RoomID,
			CurrentPrice:      rule.StartPrice,
			EndTime:           rule.EndTime,
			BidIncrement:      rule.BidIncrement,
			PriceCap:          rule.PriceCap,
			DepositAmount:     rule.DepositAmount,
			ExtendTriggerSec:  s.policy.ExtendTriggerSec,
			AutoExtendSec:     s.policy.AutoExtendSec,
			MaxExtendCount:    s.policy.MaxExtendCount,
			MaxTotalExtendSec: s.policy.MaxTotalExtendSec,
		}
		if err := s.cache.InitAuctionState(ctx, item.ID, state); err != nil {
			return err
		}
		if err := s.cache.ScheduleAuctionEnd(ctx, item.ID, rule.EndTime.UnixMilli()); err != nil {
			_ = s.cache.DeleteAuctionState(ctx, item.ID)
			return err
		}
	}
	item.Status = model.ItemOngoing
	if err := s.store.UpdateItemWithRule(item, rule); err != nil {
		if s.cache != nil {
			_ = s.cache.UnscheduleAuctionEnd(ctx, item.ID)
			_ = s.cache.DeleteAuctionState(ctx, item.ID)
		}
		return err
	}
	if err := s.store.SetRoomCurrentItem(item.RoomID, item.ID); err != nil {
		if s.cache != nil {
			_ = s.cache.UnscheduleAuctionEnd(ctx, item.ID)
			_ = s.cache.DeleteAuctionState(ctx, item.ID)
		}
		return err
	}
	if s.cache != nil {
		_ = s.cache.SetRoomCurrentItem(ctx, item.RoomID, item.ID)
	}
	if s.broadcaster != nil {
		now := s.now()
		_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
			Type: dto.EventAuctionStarted,
			Payload: dto.AuctionStartedPayload{
				ItemID:           item.ID,
				RoomID:           item.RoomID,
				StartTime:        now,
				EndTime:          rule.EndTime,
				ServerTimeUnixMS: now.UnixMilli(),
				EndTimeUnixMS:    rule.EndTime.UnixMilli(),
				AuctionVersion:   0,
			},
		})
	}
	return nil
}
```

The helper must contain the exact rollback behavior currently inside `StartItem`.

Add:

```go
func (s *Service) StartDueAuctions(ctx context.Context) {
	var err error
	startedCount := 0
	finish := observability.Track(ctx, "item.start_due_auctions")
	defer func() {
		finish(&err, "started_count", startedCount)
	}()
	items, listErr := s.store.ListPublishedItemsPastStartTime(s.now(), 50)
	if listErr != nil {
		err = listErr
		return
	}
	for _, iwr := range items {
		if startErr := s.startItemWithRule(ctx, iwr.Item, iwr.Rule); startErr != nil {
			logx.Warnw("item.StartDueAuctions start failed", "item_id", iwr.Item.ID, "err", startErr)
			continue
		}
		startedCount++
	}
}
```

- [ ] **Step 5: Register start cron**

In `internal/app/item/init.go`, add before settlement cron:

```go
	engine.Cron.AddFunc("@every 1s", observability.WrapCron("item.start_due_auctions", svc.StartDueAuctions))
```

- [ ] **Step 6: Run GREEN automatic-start test**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run TestStartDueAuctionsStartsPublishedItemsPastStartTime -count=1
```

Expected: PASS.

- [ ] **Step 7: Write failing non-winner refund tests**

In `internal/app/item/service/service_test.go`, extend fake deposit checker or create:

```go
type fakeDepositService struct {
	paid map[string]bool
	refundNonWinnersCalls []string
	refundNonWinnersErr error
}

func (s *fakeDepositService) HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error) {
	return s.paid[itemID+"\x00"+userID], nil
}

func (s *fakeDepositService) RefundNonWinners(ctx context.Context, itemID, winnerUserID string) (depositservice.SettlementSummary, error) {
	s.refundNonWinnersCalls = append(s.refundNonWinnersCalls, itemID+"\x00"+winnerUserID)
	return depositservice.SettlementSummary{Refunded: 1}, s.refundNonWinnersErr
}
```

Add import:

```go
depositservice "github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
```

Append a settlement test:

```go
func TestSettleDueAuctionsRefundsNonWinnerDeposits(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	deposits := &fakeDepositService{paid: map[string]bool{}}
	svc := NewService(store, itemdto.DefaultAuctionPolicy(), cache, nil, deposits, nil)
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	item := &itemmodel.AuctionItem{ID: "item_1", MerchantID: "merchant_1", RoomID: "room_1", RuleID: "rule_1", Status: itemmodel.ItemPublished}
	rule := &itemmodel.AuctionRule{ID: "rule_1", ItemID: "item_1", StartPrice: 1000, BidIncrement: 100, StartTime: now.Add(-time.Minute), EndTime: now.Add(-time.Second)}
	if err := store.CreateItemWithRule(item, rule); err != nil {
		t.Fatalf("CreateItemWithRule: %v", err)
	}
	if err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, "item_1"); err != nil {
		t.Fatalf("StartItem: %v", err)
	}
	cache.states["item_1"].LeaderUserID = "winner_1"
	cache.states["item_1"].DealPrice = 1500
	cache.states["item_1"].EndTimeUnixMS = now.Add(-time.Second).UnixMilli()
	cache.ending["item_1"] = now.Add(-time.Second).UnixMilli()

	svc.SettleDueAuctions(context.Background())

	if len(deposits.refundNonWinnersCalls) != 1 || deposits.refundNonWinnersCalls[0] != "item_1\x00winner_1" {
		t.Fatalf("expected non-winner refunds for winner_1, got %#v", deposits.refundNonWinnersCalls)
	}
}
```

- [ ] **Step 8: Run RED non-winner refund test**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run TestSettleDueAuctionsRefundsNonWinnerDeposits -count=1
```

Expected: FAIL because `DepositChecker` does not include `RefundNonWinners` and settlement does not call it.

- [ ] **Step 9: Extend item deposit interface and settlement hooks**

In `internal/app/item/service/service.go`, import:

```go
depositservice "github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
```

Replace `DepositChecker` with:

```go
type DepositService interface {
	HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error)
	RefundNonWinners(ctx context.Context, itemID, winnerUserID string) (depositservice.SettlementSummary, error)
}
```

Change the service field from `depositSvc DepositChecker` to:

```go
	depositSvc DepositService
```

Change `NewService` parameter type to `depositSvc DepositService`.

In `persistSettledAuction`, after cache cleanup and before or after broadcasts, add:

```go
	if s.depositSvc != nil {
		if _, refundErr := s.depositSvc.RefundNonWinners(ctx, item.ID, result.LeaderUserID); refundErr != nil {
			logx.Warnw("item.persistSettledAuction refund non-winners failed", "item_id", item.ID, "winner_user_id", result.LeaderUserID, "err", refundErr)
		}
	}
```

In `internal/app/item/service/bid_service.go`, in the price-cap settlement path after cache cleanup and before order creation, add:

```go
		if s.depositSvc != nil {
			if _, refundErr := s.depositSvc.RefundNonWinners(ctx, item.ID, current.ID); refundErr != nil {
				logx.Warnw("item.PlaceBid refund non-winners failed", "item_id", item.ID, "winner_user_id", current.ID, "err", refundErr)
			}
		}
```

- [ ] **Step 10: Run GREEN item service tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -count=1
```

Expected: PASS.

- [ ] **Step 11: Commit item lifecycle changes**

Run:

```bash
rtk git add internal/app/item/dao/item.go internal/app/item/service/service.go internal/app/item/service/bid_service.go internal/app/item/service/service_test.go internal/app/item/service/bid_service_test.go internal/app/item/init.go
rtk git commit -m "feat: automate auction start and refund non-winners"
```

## Task 4: E2E Contract and P0 Docs

**Files:**
- Modify `docs/agent-testing/flows/auction-lifecycle.md`
- Modify `docs/agent-testing/modules/deposit.md`
- Modify `docs/agent-testing/modules/order.md`
- Modify `docs/agent-testing/modules/payment.md`
- Modify `docs/todo.md`

- [ ] **Step 1: Update auction lifecycle flow**

Edit `docs/agent-testing/flows/auction-lifecycle.md`:

- Remove order payment from the "not covered" list.
- Add payment module to covered modules.
- Add steps after auction settlement:

```text
18. 查询用户 B 的订单列表或订单详情，确认生成 pending 订单。
19. 验证用户 A 的保证金状态为 refunded。
20. 用户 B 支付订单。
21. 验证订单状态为 paid。
22. 验证用户 B 的保证金状态为 refunded。
23. 在独立违约分支中创建另一件本批次成交拍品，赢家取消订单或等待订单过期扫描。
24. 验证违约赢家保证金状态为 forfeited。
```

Add Then assertions:

```text
- 竞拍结束后非赢家保证金必须进入 refunded。
- 赢家订单支付前，赢家保证金保持 paid。
- 赢家支付订单后，赢家保证金进入 refunded。
- 赢家取消订单或订单过期后，赢家保证金进入 forfeited。
- 支付、取消或过期失败时，保证金终态不得被覆盖。
```

- [ ] **Step 2: Update module docs**

In `docs/agent-testing/modules/deposit.md`, add to business rules:

```text
- 竞拍结算后，非赢家 paid 保证金进入 refunded。
- 有赢家时，赢家 paid 保证金保持 paid，直到订单 paid/cancelled/expired。
- 订单 paid 后，赢家 paid 保证金进入 refunded。
- 订单 cancelled 或 expired 后，赢家 paid 保证金进入 forfeited。
- refunded 和 forfeited 是终态，不被后续结算覆盖。
- 当前模型复用 refunded_at 表示 refunded/forfeited 的终态结算时间。
```

In `docs/agent-testing/modules/order.md`, add to order transition rules:

```text
- pending -> paid 成功后触发赢家保证金退款。
- pending -> cancelled 成功后触发赢家保证金罚没。
- pending -> expired 成功后触发赢家保证金罚没。
- 保证金结算失败不回滚已提交订单状态，但必须记录为风险证据。
```

In `docs/agent-testing/modules/payment.md`, add to payment boundaries:

```text
- 当前支付成功仍不调用真实第三方支付，但会触发赢家保证金从 paid 进入 refunded。
- 当前取消订单会触发赢家保证金从 paid 进入 forfeited。
```

- [ ] **Step 3: Update docs/todo.md**

After tests pass, update `docs/todo.md`:

- Mark automatic start complete.
- Mark automatic end real-time complete because `SettleDueAuctions` is already `@every 1s`; leave fallback note for `EndExpiredAuctions`.
- Mark deposit release strategy complete.
- Mark order compensation explanation complete if the implementation/docs state the compensation boundary.
- Leave full E2E report unchecked until an approved E2E run is executed and report is written.

- [ ] **Step 4: Run docs sanity checks**

Run:

```bash
rtk rg -n "订单支付。|不覆盖范围" docs/agent-testing/flows/auction-lifecycle.md
rtk rg -n "refunded|forfeited|保证金" docs/agent-testing/flows/auction-lifecycle.md docs/agent-testing/modules/deposit.md docs/agent-testing/modules/order.md docs/agent-testing/modules/payment.md docs/todo.md
```

Expected: first command does not show order payment as an out-of-scope bullet; second command shows the new terminal-state rules.

- [ ] **Step 5: Commit docs**

Run:

```bash
rtk git add docs/agent-testing/flows/auction-lifecycle.md docs/agent-testing/modules/deposit.md docs/agent-testing/modules/order.md docs/agent-testing/modules/payment.md docs/todo.md
rtk git commit -m "docs: update p0 auction lifecycle contract"
```

## Task 5: Final Verification

**Files:**
- Verify all modified files.

- [ ] **Step 1: Run formatting**

Run:

```bash
rtk gofmt -w internal/app/deposit/dao/deposit.go internal/app/deposit/service/service.go internal/app/deposit/service/service_test.go internal/app/deposit/init.go internal/app/order/service/service.go internal/app/order/service/cron.go internal/app/order/service/service_test.go internal/app/item/dao/item.go internal/app/item/service/service.go internal/app/item/service/bid_service.go internal/app/item/service/service_test.go internal/app/item/service/bid_service_test.go internal/app/item/init.go
```

Expected: command exits 0.

- [ ] **Step 2: Run focused package tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/deposit/... ./internal/app/order/... ./internal/app/item/... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run full tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...
```

Expected: PASS.

- [ ] **Step 4: Run git diff checks**

Run:

```bash
rtk git diff --check
rtk git status --short
```

Expected: `git diff --check` exits 0. `git status --short` lists only intended modified or staged files before the final commit, or is clean after the final commit.

- [ ] **Step 5: Commit final verification adjustments**

If formatting or docs changed after previous commits, run:

```bash
rtk git add internal docs
rtk git commit -m "chore: verify p0 auction closure"
```

If there are no changes, skip this commit.

## Spec Coverage Review

- Automatic start: Task 3 implements DAO scan, service worker, cron registration, and tests.
- Deposit release strategy: Task 1 implements deposit terminal transitions; Task 3 calls non-winner refund; Task 2 calls winner refund/forfeit.
- Order payment/cancel/expiry: Task 2 hooks all committed order transitions.
- E2E contract: Task 4 updates lifecycle and module docs; execution remains approval-gated.
- Observability: Tasks 1, 2, and 3 use existing `observability.Track` or `logx.Warnw` for new operations.
- Local test boundary: all RED/GREEN tests use fake stores and do not connect to MySQL, Redis, HTTP, WebSocket, or external systems.

## Execution Notes

- Worktree: `/Users/echin/echin/go/live-auction-backend/.worktrees/p0-auction-demo-closure`
- Branch: `codex/p0-auction-demo-closure`
- Use `rtk` for every command.
- Use `GOCACHE=/tmp/live-auction-go-cache` for Go tests to avoid sandbox writes to the default Go cache.
- Do not run dependency-backed E2E without explicit user approval under `skills/agent-testing-gate/SKILL.md`.
