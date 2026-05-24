# Order & Payment Module Design

Date: 2026-05-24  
Status: Approved

## Context

Live-auction backend (`internal/app/`) currently has: `user`, `item`, `room` modules.  
This document covers the design for adding `order` and `payment` modules.

Reference spec: `docs/5-21.md` sections 7–8, 10.2, 11.

## Key Decisions

| Question | Decision |
|----------|----------|
| Order expiry | Cron job scans `expired_at < now`, batch-updates to `expired` |
| Module split | Two separate modules: `order` + `payment` |
| Cross-module trigger | `item` imports `order/service` directly; no event bus |
| CreateOrder failure recovery | Async compensation cron scans `ended` items with no order |
| Payment logic ownership | `order.Service` owns state machine; `payment` module is a thin handler |

## Module Structure

```
internal/app/
├── order/
│   ├── model/     order.go         — Order struct, OrderStatus constants
│   ├── dao/       order.go         — Store interface + GormStore
│   ├── dto/       order.go         — request/response/Input types
│   ├── service/   service.go       — state machine, CreateOrder, Pay, Cancel, expiry scan
│   ├── handler/   order.go         — GET /orders, GET /orders/{id}
│   ├── router/    router.go
│   └── init.go                     — registers cron jobs, injects svc
│
└── payment/
    ├── handler/   payment.go       — POST /orders/{id}/pay, POST /orders/{id}/cancel
    ├── router/    router.go
    └── init.go                     — receives orderSvc, injects into handler
```

**Module registration order** (`appInitialize/`): `order` → `payment` → `item`  
This ensures `order.Service` is ready before `payment` and `item` wire up to it.

## Dependency Graph

```
item module    ──→  order.Service  (CreateOrder)
payment module ──→  order.Service  (Pay, Cancel)
order module   ──→  order.dao.Store
```

No circular imports. `payment` and `item` only import `order/service`, never each other.

## Data Model

```go
// order/model/order.go

type OrderStatus string

const (
    OrderPending   OrderStatus = "pending"
    OrderPaid      OrderStatus = "paid"
    OrderCancelled OrderStatus = "cancelled"
    OrderExpired   OrderStatus = "expired"
)

type Order struct {
    ID        string      `gorm:"primaryKey;size:64" json:"id"`
    ItemID    string      `gorm:"index;size:64;not null" json:"item_id"`
    UserID    string      `gorm:"index;size:64;not null" json:"user_id"`
    Price     int64       `gorm:"not null" json:"price"`
    Status    OrderStatus `gorm:"index;size:32;not null" json:"status"`
    ExpiredAt time.Time   `gorm:"index" json:"expired_at"`
    CreatedAt time.Time   `json:"created_at"`
    UpdatedAt time.Time   `json:"updated_at"`
}
```

- No `MerchantID` field. Merchant queries use `JOIN auction_items ON orders.item_id = items.id WHERE items.merchant_id = ?`.
- `ExpiredAt` is indexed for efficient cron scans.
- Default payment timeout: **30 minutes** (`ExpiredAt = CreatedAt + 30min`), configurable via `config.yaml`.

## DAO Store Interface

```go
type Store interface {
    AutoMigrate() error
    CreateOrder(order *model.Order) error
    FindOrder(orderID string) (*model.Order, error)
    FindOrderByItemID(itemID string) (*model.Order, error)      // idempotency + compensation
    UpdateOrderStatus(orderID string, from, to model.OrderStatus) (bool, error) // CAS
    ListOrders(input dto.ListOrdersInput) ([]model.Order, int64, error)
    ListExpiredPendingOrders(before time.Time, limit int) ([]model.Order, error)
}
```

`UpdateOrderStatus` uses a conditional UPDATE for idempotency:

```sql
UPDATE orders SET status = $to, updated_at = now() WHERE id = $id AND status = $from
```

Returns `(true, nil)` when `RowsAffected == 1`, `(false, nil)` when `== 0` (already transitioned or concurrent race).

## State Machine

```
pending ──→ paid        (user pays)
pending ──→ cancelled   (user cancels)
pending ──→ expired     (cron scans timeout)
```

`paid`, `cancelled`, `expired` are terminal states — no further transitions allowed.

## Service Methods

### CreateOrder

```go
func (s *Service) CreateOrder(itemID, userID string, price int64) (*model.Order, error)
```

1. `FindOrderByItemID(itemID)` — if order exists, return it (idempotent; protects against compensation cron re-entry).
2. Create `Order{Status: pending, ExpiredAt: now + PaymentTimeout}`.
3. ID: `"order_" + snowflake.MakeUUID()`.

### Pay

```go
func (s *Service) Pay(current *usermodel.User, orderID string) error
```

1. `FindOrder(orderID)` — verify `order.UserID == current.ID`, else `ErrUnauthorized`.
2. `UpdateOrderStatus(orderID, pending, paid)`.
3. If `RowsAffected == 0`: re-fetch order; if already `paid` return `nil` (idempotent); otherwise return `ErrInvalidRequest`.
4. TODO: broadcast `order_paid` notification (deferred until notification module).

### Cancel

```go
func (s *Service) Cancel(current *usermodel.User, orderID string) error
```

1. `FindOrder(orderID)` — verify ownership.
2. `UpdateOrderStatus(orderID, pending, cancelled)`.
3. If `RowsAffected == 0`: return `ErrInvalidRequest` (non-pending orders cannot be cancelled).

## Cron Jobs (registered in order/init.go)

### Expiry Scanner — every 5 minutes

```go
engine.Cron.AddFunc("@every 5m", func() {
    orders, _ := store.ListExpiredPendingOrders(time.Now(), 100)
    for _, o := range orders {
        store.UpdateOrderStatus(o.ID, model.OrderPending, model.OrderExpired)
    }
})
```

### Compensation Scanner — every 10 minutes

Scans `auction_items` where `status = 'ended' AND winner_id != ''` and no corresponding row exists in `orders`. Calls `CreateOrder` for each missing order.

Uses `kernel.DB` directly (raw GORM on `auction_items` table) — does **not** import the `item` package.

```sql
SELECT ai.id, ai.winner_id, ai.deal_price
FROM auction_items ai
LEFT JOIN orders o ON o.item_id = ai.id
WHERE ai.status = 'ended'
  AND ai.winner_id != ''
  AND o.id IS NULL
LIMIT 50
```

## APIs

### order module

| Method | Path | Auth |
|--------|------|------|
| GET | `/api/v1/orders` | login required |
| GET | `/api/v1/orders/{order_id}` | login required |

**GET /api/v1/orders**

Query params: `status`, `page`, `page_size`

Permission: merchants filter by `merchant_id` (JOIN items); regular users filter by `user_id`.

Response includes `item_title` (joined from `auction_items`) to avoid extra round-trips.

```json
{
  "code": 0, "message": "ok",
  "data": {
    "list": [
      {
        "id": "order_123",
        "item_id": "item_123",
        "item_title": "翡翠手镯",
        "user_id": "user_456",
        "price": 96000,
        "status": "pending",
        "expired_at": "2026-05-21T20:40:00+08:00",
        "created_at": "2026-05-21T20:10:00+08:00"
      }
    ],
    "page": 1, "page_size": 20, "total": 1
  }
}
```

**GET /api/v1/orders/{order_id}**

Validates ownership (user sees own; merchant sees orders from own items). Same fields as list item plus `updated_at`.

### payment module

| Method | Path | Auth |
|--------|------|------|
| POST | `/api/v1/orders/{order_id}/pay` | login required |
| POST | `/api/v1/orders/{order_id}/cancel` | login required |

**POST /api/v1/orders/{order_id}/pay**

Request body: `{ "result": "success" }` (field reserved for future real gateway integration)

**POST /api/v1/orders/{order_id}/cancel**

No request body.

Both return `{ "code": 0, "message": "ok", "data": null }` on success.

### Error Mapping

| Scenario | errorx |
|----------|--------|
| Order not found | `ErrNotFound` |
| Not the order owner | `ErrUnauthorized` |
| State transition not allowed | `ErrInvalidRequest` |

## Integration with item module

Two callsites in the item module trigger `orderSvc.CreateOrder`:

1. **`bid_service.go` — price cap hit** (`luaResult.IsCapped == true`): call `CreateOrder` synchronously after updating item status to `ended`. Failure is logged and non-fatal (compensation cron will retry).

2. **Auction-end cron** (in `item/init.go`): scans `ongoing` items past `end_time`, marks them `ended`, calls `CreateOrder` for items with a `winner_id`. Same non-fatal failure policy.

**Service wiring**: `order/init.go` exposes a package-level `var Svc *service.Service`. `item/init.go` imports the `order` package and reads `order.Svc` during `Load()`. Registration order (`order` → `item`) guarantees `Svc` is set before `item.Load()` is called. This mirrors the existing pattern where item router imports `userhandler` to access the authenticated user middleware.

## Testing Strategy

- `order/service` tests use a `fakeStore` (implements `dao.Store` with in-memory maps), same pattern as `item/service/service_test.go`.
- Inject `now func() time.Time` into `Service` for deterministic expiry and timeout tests.
- Test cases must cover: idempotent `CreateOrder`, `Pay` on already-paid order, `Cancel` on non-pending order, expiry cron batch update.
