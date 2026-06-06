# P0 Auction Demo Closure Design

## Goal

Close the P0 demo path for live auction:

```text
merchant opens room
-> item is published
-> auction starts automatically at rule.start_time
-> users pay deposits and bid
-> auction settles
-> winner receives an order
-> winner pays or defaults
-> deposits are refunded or forfeited
-> E2E evidence is recorded
```

This closes the `docs/todo.md` P0 gaps without turning the current mock payment flow into a full financial ledger.

## Chosen Approach

Use the existing module boundaries:

- `item.Service` owns auction lifecycle automation.
- `deposit.Service` owns deposit refund and forfeiture.
- `order.Service` owns order status transitions and calls deposit settlement hooks after committed transitions.
- `docs/agent-testing/flows/auction-lifecycle.md` owns the full user-visible E2E contract.

Deposits are not applied as payment discounts in this phase. They are participation and fulfillment guarantees:

- Non-winners are refunded after auction settlement.
- Winners are refunded after successful order payment.
- Winners are forfeited after order cancellation or order expiry.

## Current State

Already implemented:

- `StartItem` initializes Redis auction state, schedules Redis ending, sets room current item, and broadcasts `auction_started`.
- `SettleDueAuctions` runs every second and settles Redis-due auctions.
- `EndExpiredAuctions` remains as a slower MySQL fallback.
- Settled auctions update item final state, clear room current item, create an order for the winner, and broadcast `auction_ended` / `order_created`.
- `order.scan_expired_orders` marks expired pending orders.
- `order.scan_compensation` creates missing orders for ended items with winners.
- `Deposit` already has `paid`, `refunded`, and `forfeited` statuses.

Missing:

- No automatic start scan for `published` items whose `rule.start_time` has arrived.
- No deposit release method writes terminal deposit state.
- No winner deposit hook runs after order payment, cancellation, or expiry.
- No auction settlement hook refunds non-winners.
- The auction lifecycle E2E contract still says order payment is out of scope, which conflicts with the P0 demo goal.

## Automatic Auction Start

Add an automatic start path to `item.Service`:

```text
StartDueAuctions(ctx)
  -> store.ListPublishedItemsPastStartTime(now, limit)
  -> for each item, call a shared start helper
```

The shared start helper contains the existing `StartItem` transition work so manual HTTP start and cron automatic start cannot drift. The HTTP path still authorizes the merchant before calling the helper. The cron path uses persisted item data and does not fabricate a user.

Selection rule:

```text
auction_items.status = 'published'
auction_rules.start_time <= now
auction_items.deleted_at IS NULL
limit = 50 per run
```

Cron cadence:

```text
@every 1s
```

This matches the existing one-second settlement and time-sync cadence. If later load requires it, the scan can move to a Redis schedule, but P0 should favor visible demo correctness.

Failure handling:

- A failed item logs a warning and does not block the rest of the batch.
- Redis initialization failure keeps the item `published`.
- MySQL update or room current-item failure rolls back Redis auction state and ending schedule, matching current `StartItem` behavior.
- Re-running the cron is safe because only `published` items are selected.

## Deposit Settlement Rules

### Auction Settlement

When an auction reaches final state:

- If there is no winner, refund every paid deposit for the item.
- If there is a winner, refund every paid deposit except the winner's deposit.
- The winner's paid deposit remains `paid` until the order reaches `paid`, `cancelled`, or `expired`.

The item service calls the deposit service after final auction persistence succeeds. Deposit release is non-fatal for auction settlement: update failure is logged and observable, but it must not undo final auction result or order creation. The operation is idempotent so retries are safe.

### Order Payment

When `order.Pay` successfully changes an order from `pending` to `paid`, the winner's deposit for that item becomes:

```text
paid -> refunded
refunded -> no-op
forfeited -> no-op with warning
missing -> no-op with warning
```

This represents returning the guarantee after the winner fulfilled payment.

### Order Cancellation

When `order.Cancel` successfully changes an order from `pending` to `cancelled`, the winner's deposit for that item becomes:

```text
paid -> forfeited
forfeited -> no-op
refunded -> no-op with warning
missing -> no-op with warning
```

This represents buyer default after winning.

### Order Expiry

When `ScanExpiredOrders` successfully changes a pending order to `expired`, the winner's deposit follows the same forfeiture rule as cancellation.

### Idempotency and State Safety

Terminal deposit states are not overwritten:

- `refunded` stays `refunded`.
- `forfeited` stays `forfeited`.
- Only `paid` records transition to a terminal state.

This prevents order cron, retries, cancellation, and future compensation from fighting over the same row.

## Deposit Service API

Add explicit service methods instead of letting other modules mutate deposit rows:

```go
type SettlementSummary struct {
    Refunded  int
    Forfeited int
    Skipped   int
}

func (s *Service) RefundNonWinners(ctx context.Context, itemID, winnerUserID string) (SettlementSummary, error)
func (s *Service) RefundWinner(ctx context.Context, itemID, userID string) (SettlementSummary, error)
func (s *Service) ForfeitWinner(ctx context.Context, itemID, userID string) (SettlementSummary, error)
```

DAO additions:

```go
ListPaidDepositsByItem(itemID string) ([]model.Deposit, error)
TransitionDepositStatus(itemID, userID string, from, to model.DepositStatus, terminalAt *time.Time) (bool, error)
```

`refunded_at` is set for both `refunded` and `forfeited` terminal transitions because the model currently has only one terminal timestamp. In docs and reports, interpret it as terminal settlement time until a future ledger adds separate `forfeited_at`.

## Order Service Wiring

`order.Service` receives an optional dependency:

```go
type DepositSettler interface {
    RefundWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error)
    ForfeitWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error)
}
```

Use a `SetDepositSettler` method if constructor injection is awkward because module initialization order already exists. Hook points:

- `Pay`: after `UpdateOrderStatus(pending, paid)` succeeds.
- `Cancel`: after `UpdateOrderStatus(pending, cancelled)` succeeds.
- `ScanExpiredOrders`: inside the loop, after `UpdateOrderStatus(pending, expired)` returns `ok=true`.

Deposit settlement errors are logged but do not turn a committed order status transition into a failed HTTP response.

## Item Service Wiring

Extend the current deposit dependency so item service can both check and release deposits:

```go
type DepositService interface {
    HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error)
    RefundNonWinners(ctx context.Context, itemID, winnerUserID string) (depositservice.SettlementSummary, error)
}
```

Hook points:

- `persistSettledAuction`: after item final state and room cleanup are persisted, call `RefundNonWinners`.
- Price-cap settlement in `PlaceBid`: after item final state and room cleanup are persisted, call `RefundNonWinners`.

Refund failures continue order creation and broadcasts. The error is logged with `item_id` and `winner_user_id`.

## E2E Contract Update

Update `docs/agent-testing/flows/auction-lifecycle.md` so the main flow includes:

1. Users A and B pay deposits.
2. Users A and B bid; B wins.
3. Auction settles and creates a pending order for B.
4. A's deposit becomes `refunded`.
5. B pays the order.
6. B's deposit becomes `refunded`.
7. A separate default branch creates or reuses another winning order and verifies cancel or expiry causes the winner deposit to become `forfeited`.

The E2E run itself is not executed as part of implementation without explicit approval. It must follow:

```text
docs/agent-testing/README.md
-> templates/protocol.md
-> guides/runner.md
-> flows/auction-lifecycle.md
-> modules/user.md, room.md, item.md, deposit.md, bid.md, order.md, payment.md, ws.md
-> reports/README.md
```

## Testing Strategy

Use TDD for production code changes.

Local unit tests:

- `deposit/service`: refund non-winners, refund winner, forfeit winner, idempotent terminal behavior, missing deposit no-op.
- `order/service`: paid order refunds winner deposit; cancelled order forfeits winner deposit; expired scan forfeits winner deposit; already paid idempotent pay does not double-settle.
- `item/service`: automatic start selects due published items and skips future items; auto start reuses start behavior; settlement refunds non-winners.

Docs and contract tests:

- Flow contract reflects order payment and deposit terminal states.
- `docs/todo.md` marks automatic start, real-time end, deposit release strategy, order compensation explanation, and E2E report status accurately after implementation and test evidence.

Dependency-backed E2E:

- Requires explicit approval before running.
- Uses only current test batch data.
- Produces a report under `docs/agent-testing/reports/`.
- Redacts online addresses, credentials, tokens, and full WebSocket tickets.

## Observability

Use existing `observability.Track` for new service methods:

- `item.start_due_auctions`
- `deposit.refund_non_winners`
- `deposit.refund_winner`
- `deposit.forfeit_winner`

Record item ID, winner/user ID where applicable, result counts, and error status.

Cron name:

- `item.start_due_auctions`

Existing order cron names stay unchanged.

## Risks and Trade-Offs

- Deposit settlement after order status change can partially fail. This is acceptable for P0 if logged and idempotent, but a later compensation job should scan stuck `paid` deposits on ended items.
- `refunded_at` is reused for forfeiture terminal time. This avoids schema expansion now, but future finance work should add separate fields or a ledger table.
- Automatic start scans MySQL every second. This is fine for demo scale, but a Redis start schedule would be better for large catalogs.
- Dependency-backed E2E execution must remain approval-gated.

## Acceptance Criteria

- A published item with `rule.start_time <= now` starts automatically without HTTP manual start.
- A published item with future `rule.start_time` remains published.
- Auction settlement refunds paid deposits for all non-winning users.
- Winner deposit remains paid after auction settlement and before order resolution.
- Paying the winner order refunds the winner deposit.
- Cancelling or expiring the winner order forfeits the winner deposit.
- Deposit settlement methods are idempotent and never overwrite terminal statuses.
- Unit tests pass without MySQL, Redis, HTTP, WebSocket, or external services.
- The E2E flow contract includes auction settlement, order creation, order payment, and deposit terminal-state assertions.
- Dependency-backed E2E execution is documented but not run without explicit user approval.
