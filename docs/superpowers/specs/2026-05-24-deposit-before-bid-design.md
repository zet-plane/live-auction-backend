# Deposit Before Bid Module Design

Date: 2026-05-24
Status: Approved

## Context

The item module already stores `AuctionRule.DepositAmount`, but bidding does not
check whether the bidder has paid a deposit. The order and payment modules now
cover post-auction order payment. Deposit handling should stay separate from
order handling so the order module remains focused on final auction orders.

## Decision

Add a separate `deposit` module.

The deposit module owns deposit records, deposit payment state, and the question
"can this user bid on this item?" The item module asks the deposit service before
placing a bid. The order module is not used for deposit records in this phase.

## Goals

- Require a paid deposit before bidding when `AuctionRule.DepositAmount > 0`.
- Keep `DepositAmount == 0` items compatible with the current bid flow.
- Make deposit payment idempotent for the same `item_id + user_id`.
- Keep service logic testable through DAO interfaces and fake stores.
- Avoid coupling deposit lifecycle to final auction order lifecycle for now.

## Non-Goals

- Real third-party payment gateway integration.
- Automatic refund or forfeiture settlement.
- Deposit deduction from final order price.
- Merchant-side deposit reports.
- Changing the order state machine.

## Module Structure

```text
internal/app/deposit/
  model/     deposit.go
  dao/       deposit.go
  dto/       deposit.go
  service/   service.go
  handler/   deposit.go
  router/    deposit.go
  init.go
```

The module follows the existing app pattern: `init.go` creates a GORM store,
runs migrations, creates a service, initializes handlers, and registers routes.

## Data Model

```go
type DepositStatus string

const (
    DepositPending   DepositStatus = "pending"
    DepositPaid      DepositStatus = "paid"
    DepositRefunded  DepositStatus = "refunded"
    DepositForfeited DepositStatus = "forfeited"
)

type Deposit struct {
    ID         string
    ItemID     string
    UserID     string
    Amount     int64
    Status     DepositStatus
    PaidAt     *time.Time
    RefundedAt *time.Time
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

Indexes:

- Unique composite index on `item_id, user_id`.
- Secondary indexes on `item_id`, `user_id`, and `status`.

Amount values use fen, matching the existing project money convention.

## Service API

```go
func (s *Service) PayDeposit(current *usermodel.User, itemID string) (*dto.DepositDetail, error)
func (s *Service) GetMyDeposit(current *usermodel.User, itemID string) (*dto.DepositDetail, error)
func (s *Service) HasPaidDeposit(itemID, userID string, requiredAmount int64) (bool, error)
```

`PayDeposit` is a local simulated payment:

1. Normalize `itemID`.
2. Reject missing user or missing item ID.
3. Read the item's required deposit amount from `auction_rules.deposit_amount`.
4. Reject payment when the item does not require a deposit.
5. Find existing deposit by `item_id + user_id`.
6. If it is already `paid` and `amount >= required amount`, return it.
7. If it exists but is not terminal, update it to `paid`, set amount and `paid_at`.
8. If it does not exist, create a `paid` deposit.

`HasPaidDeposit` returns true only when a deposit exists with:

- same `item_id`
- same `user_id`
- `status = paid`
- `amount >= requiredAmount`

If `requiredAmount <= 0`, it returns true.

## Item Integration

`item.Service` receives an optional `depositSvc` dependency, similar to the
current `orderSvc` dependency.

`PlaceBid` checks deposit status after loading item and rule, before calling the
Redis Lua bidding script:

```text
FindItemWithRule
validate item is ongoing
if rule.DepositAmount > 0:
  require depositSvc != nil
  require depositSvc.HasPaidDeposit(item.ID, current.ID, rule.DepositAmount)
call Redis PlaceBidLua
write BidLog
handle price cap and final order creation
```

If the deposit service is missing while the item requires a deposit, return
`errorx.ErrInternal`. If the user has not paid the required deposit, return
HTTP 400 code `40005` with message `deposit required`.

## API

Routes are mounted under the item resource because users think of deposits as
joining an item's auction.

| Method | Path | Auth | Behavior |
| --- | --- | --- | --- |
| POST | `/api/v1/items/{item_id}/deposit/pay` | login required | Pay or idempotently return the current user's deposit |
| GET | `/api/v1/items/{item_id}/deposit` | login required | Return current user's deposit status for the item |

`POST /deposit/pay` derives the amount from `AuctionRule.DepositAmount`, not
from the request body. The deposit service reads the item rule through a narrow
DAO query. This prevents clients from paying a lower amount than required.

Response shape:

```json
{
  "id": "deposit_123",
  "item_id": "item_123",
  "user_id": "user_123",
  "amount": 5000,
  "status": "paid",
  "paid_at": "2026-05-24T12:00:00+08:00",
  "refunded_at": null,
  "created_at": "2026-05-24T12:00:00+08:00",
  "updated_at": "2026-05-24T12:00:00+08:00"
}
```

## Cross-Module Wiring

Registration order should place `deposit` before `item`, so `item.Load()` can
read `deposit.Svc` and pass it into `item.NewService`.

```text
user -> room -> order -> payment -> deposit -> item
```

This keeps item-to-deposit dependency one-way:

```text
item service -> deposit service
deposit service -> deposit dao
```

The first implementation uses a narrow store query in the deposit module to
fetch `auction_rules.deposit_amount` by `item_id`, avoiding an import cycle
with `item/service`.

## Error Mapping

| Scenario | Error |
| --- | --- |
| Item not found while paying deposit | `ErrNotFound` |
| Deposit amount required but service is unavailable | `ErrInternal` |
| User bids without paid deposit | HTTP 400 code `40005`, message `deposit required` |
| Deposit has lower amount than required | HTTP 400 code `40005`, message `deposit required` |
| User requests deposit status before paying | `ErrNotFound` |

## Testing Strategy

Deposit service tests use a fake store and fixed time:

- `PayDeposit` creates a paid deposit.
- Repeated `PayDeposit` for the same item and user is idempotent.
- `HasPaidDeposit` returns false when no record exists.
- `HasPaidDeposit` returns false for pending, refunded, forfeited, or underpaid deposits.
- `HasPaidDeposit` returns true for a paid deposit with sufficient amount.
- `requiredAmount <= 0` skips the check.

Item service tests extend the existing fake-store style:

- Bidding on an item with `DepositAmount == 0` still succeeds without deposit service.
- Bidding on an item with `DepositAmount > 0` fails before Redis when the user has not paid.
- Bidding on an item with `DepositAmount > 0` succeeds when the deposit service reports paid.
- The failed deposit check does not write a bid log or mutate Redis state.

## Future Work

- Refund deposits for non-winning bidders after auction end.
- Forfeit or apply the winner's deposit based on final order payment result.
- Add merchant deposit reports.
- Replace simulated deposit payment with a shared payment gateway adapter.
