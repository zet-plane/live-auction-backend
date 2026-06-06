# P0 Auction Demo Closure Design

## Goal

Make the live-auction backend demonstrably complete for the core demo path:

```text
merchant opens room
-> item is published
-> auction starts automatically at rule.start_time
-> users pay deposits and bid
-> auction settles
-> winner receives an order
-> winner pays or defaults
-> deposits are released or forfeited
-> E2E evidence is recorded
```

The feature should close the P0 gaps in `docs/todo.md` without turning the current mock payment flow into a full financial ledger.

## Chosen Approach

Use the existing module boundaries and add the smallest durable business hooks:

- `item.Service` owns auction lifecycle automation.
- `deposit.Service` owns deposit release and forfeiture.
- `order.Service` owns order status transitions and calls deposit settlement hooks after a successful transition.
- `docs/agent-testing/flows/auction-lifecycle.md` owns the full user-visible E2E contract.

Deposits are not applied as a payment discount in this phase. They are a participation and fulfillment guarantee:

- Non-winners are refunded after auction settlement.
- Winners are refunded after successful order payment.
- Winners are forfeited after order cancellation or order expiry.

This keeps the P0 demo honest and traceable while avoiding new payment ledger, refund channel, or reconciliation concepts.

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
- No deposit release method writes `refunded_at`.
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

The shared start helper should contain the existing `StartItem` transition work so HTTP manual start and cron automatic start cannot drift. The HTTP path still authorizes the merchant before calling the helper. The cron path uses the item's persisted merchant and does not need to fabricate a user.

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

Reasoning: the settlement worker and time sync already run every second, and this is a demo-first real-time auction backend. If later load requires it, the scan can move to 5 seconds or a Redis schedule, but the first P0 implementation should favor visible correctness.

Failure handling:

- A failed item logs a warning and does not block the rest of the batch.
- Redis initialization failure must keep the item `published`.
- MySQL update or room current-item failure must roll back Redis auction state and ending schedule, matching the current `StartItem` behavior.
- Re-running the cron is safe because only `published` items are selected.

## Deposit Settlement Rules

### Auction Settlement

When an auction reaches final state:

- If there is no winner, refund every paid deposit for the item.
- If there is a winner, refund every paid deposit except the winner's deposit.
- The winner's paid deposit remains `paid` until the order reaches `paid`, `cancelled`, or `expired`.

The item service should call the deposit service after final auction persistence succeeds. Deposit release is non-fatal for auction settlement: a deposit update failure should be logged and observable, but it must not undo the already-final auction result or order creation. The operation must be idempotent so compensation or retry jobs can safely call it again later.

### Order Payment

When `order.Pay` successfully changes an order from `pending` to `paid`, the winner's deposit for that item becomes:

```text
paid -> refunded
refunded -> no-op
forfeited -> no-op with warning
missing -> no-op with warning
```

This represents the platform returning the guarantee after the winner fulfilled payment.

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

When `ScanExpiredOrders` successfully changes a pending order to `expired`, the winner's deposit for that item follows the same forfeiture rule as cancellation.

### Idempotency and State Safety

Deposit terminal states are not overwritten:

- `refunded` stays `refunded`.
- `forfeited` stays `forfeited`.
- Only `paid` records transition to a terminal state.

This is important because order cron, retries, manual cancellation, and future compensation can overlap.

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
TransitionDepositStatus(itemID, userID string, from, to model.DepositStatus, refundedAt *time.Time) (bool, error)
```

`refunded_at` should be set for both `refunded` and `forfeited` terminal transitions because the model currently has only one terminal timestamp. In reports and docs, interpret it as "terminal settlement time" until a future ledger introduces separate `forfeited_at`.

## Order Service Wiring

`order.Service` should receive an optional deposit settlement dependency:

```go
type DepositSettler interface {
    RefundWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error)
    ForfeitWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error)
}
```

The order module initializer wires `depositapp.Svc` into `order.Service` after both modules are loaded. If wiring order makes direct constructor injection awkward, expose a `SetDepositSettler` method on `order.Service` and call it from the order module `Load`.

Hook points:

- `Pay`: after `UpdateOrderStatus(pending, paid)` succeeds.
- `Cancel`: after `UpdateOrderStatus(pending, cancelled)` succeeds.
- `ScanExpiredOrders`: inside the loop, after `UpdateOrderStatus(pending, expired)` returns `ok=true`.

Deposit settlement errors should be logged and recorded but not turn a successful order status transition into a failed HTTP response. The user-visible order state is already committed. Follow-up compensation can be added later if needed.

## Item Service Wiring

`item.Service` already receives `depositapp.Svc` as a `DepositChecker`. Replace or extend this dependency with an interface that includes both checking and settlement:

```go
type DepositService interface {
    HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error)
    RefundNonWinners(ctx context.Context, itemID, winnerUserID string) (depositservice.SettlementSummary, error)
}
```

Hook points:

- `persistSettledAuction`: after item final state and room cleanup are persisted, call `RefundNonWinners`.
- Price-cap settlement in `PlaceBid`: after item final state and room cleanup are persisted, call `RefundNonWinners`.

If refunding non-winners fails, continue order creation and broadcasts. Log the error with `item_id` and `winner_user_id`.

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

The implementation can update the flow contract and create a planned report path, but a dependency-backed E2E execution requires a separate user approval in the conversation.

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
- Must use only current test batch data.
- Must produce a report under `docs/agent-testing/reports/`.
- Must redact online addresses, credentials, tokens, and full WebSocket tickets.

## Observability

Use existing `observability.Track` for new service methods:

- `item.start_due_auctions`
- `deposit.refund_non_winners`
- `deposit.refund_winner`
- `deposit.forfeit_winner`

Each should record item ID, winner/user ID where applicable, result counts, and error status.

Cron names:

- `item.start_due_auctions`

Existing order cron names stay unchanged.

## Risks and Trade-Offs

- Deposit settlement after order status change can partially fail. This is acceptable for P0 if logged and idempotent, but a later P1/P2 compensation job should scan stuck `paid` deposits on ended items.
- `refunded_at` is reused for forfeiture terminal time. This avoids schema expansion now, but future finance work should add separate fields or a ledger table.
- Automatic start scans MySQL every second. This is fine for demo scale and small P0 load, but a Redis start schedule would be better for large catalogs.
- Public E2E execution depends on real or online-equivalent services and must remain approval-gated.

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
