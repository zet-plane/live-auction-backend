# WS 消息分级与 time_sync 优化实现计划

> **给 agentic workers：** 执行本计划时必须使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans`，逐项执行。步骤使用 checkbox（`- [ ]`）用于跟踪进度。

**目标：** 把 WebSocket 消息从单一 FIFO 队列改成分级投递，避免 `time_sync` 在 bid fanout 高峰中排队变旧，将 300 WS + 70 QPS 下的 `time_sync` P95 压到 1s 以下、P99 压到 1.5s 以下。

**核心设计：** WS 消息分三类处理：

| 类别 | 事件 | 语义 | 投递策略 |
| --- | --- | --- | --- |
| 高优先级业务事件 | `user_outbid`、`auction_extended`、`auction_ended`、`auction_cancelled`、`order_created`、`auction_started`、`auction_snapshot` | 业务状态变化，不能被普通 bid 刷屏挡住 | 进入 `high` 队列，写循环优先消费 |
| 最新状态事件 | `time_sync` | 只关心最新一条，历史值过期即无意义 | 不进 FIFO，只保留 latest slot，新值覆盖旧值 |
| 普通可合并事件 | `bid_success`、未知 room broadcast 事件 | 高频、可被短时间批处理吸收 | 继续走现有 normal FIFO `send` 队列 |

**架构边界：** 本计划只做单实例 Hub 内部消息调度优化，不做多实例。当前瓶颈证据不是 CPU/内存/单次 socket write，而是不同语义消息共享 FIFO。多实例需要跨实例事件总线，单独设计。

**技术栈：** Go、Gorilla WebSocket、现有 `internal/app/ws/hub`、现有 `internal/core/observability` OTel recorder、现有 agent 性能 runner。

---

## 1. 设计细节

### 1.0 事件分级说明

分级原则不是“谁更常见谁更重要”，而是按客户端语义分：

1. **高优先级业务事件：必须尽快到达，且不能被刷屏事件挡住。**

   这些事件会改变用户判断或页面关键状态：

   - `user_outbid`：当前用户被超过，必须尽快提示，否则用户会误以为自己仍领先。
   - `auction_extended`：拍卖结束时间变化，会影响倒计时和继续出价决策。
   - `auction_ended`：拍卖结束，客户端应立即停止出价入口或刷新最终状态。
   - `auction_cancelled`：拍卖取消，客户端应立即终止交互。
   - `order_created`：成交订单创建，赢家侧需要及时进入后续流程。
   - `auction_started`：拍卖开始，客户端应立即进入可出价状态。
   - `auction_snapshot`：连接建立后的首帧状态，决定客户端初始页面是否正确。

   这些事件进入 `high` 队列。`high` 事件不和 `bid_success` 共用普通队列，避免被高频普通广播压在后面。

2. **最新状态事件：只关心最新值，不需要保留历史。**

   当前只有：

   - `time_sync`：用于校准服务端时间、结束时间和状态。旧的 `time_sync` 一旦被新的覆盖，就没有继续发送的价值。

   `time_sync` 进入 latest slot。每个连接最多保留一条 pending `time_sync`，新值覆盖旧值。这样即使 bid fanout 很忙，写出的也是最新状态，而不是几秒前排队的旧状态。

3. **普通可合并事件：高频、可排队、可短时间延迟。**

   当前主要是：

   - `bid_success`：每次成功出价都会广播，但服务端已经有 100ms coalescing。客户端只需要及时看到价格推进，不要求每条都抢占关键状态事件。
   - 未知 room broadcast 事件：默认走 normal，避免把未分类的新事件误提升为高优先级。

   这些事件继续走 normal FIFO `send` 队列，保持现有 bid fanout 行为。

   重要约束：如果未来新增事件，必须先判断它属于 high、latest 还是 normal，并补 `classifyEventLane` 测试；不能默认把业务关键事件塞进 normal。

### 1.1 当前问题

当前每个连接只有一个 `send chan wsevent.Event`。`Hub.deliver` 不区分消息语义，所有事件都进入这个 FIFO：

```text
bid_success -> bid_success -> user_outbid -> time_sync -> bid_success -> ...
```

在 70 QPS bid fanout 下，`bid_success` 会被广播到 240/280/300 个 WS 连接，形成短时消息波峰。`time_sync` 虽然每秒生成一次，但如果排在大量普通事件后面，客户端看到的间隔就会变成 2-3s。

### 1.2 改良后的连接结构

每个 `Conn` 维护三条投递路径：

```go
type Conn struct {
    // 高优先级业务事件：不和 bid_success 共用队列
    high chan wsevent.Event

    // 普通事件：保留现有 send channel，主要承载 bid_success
    send chan wsevent.Event

    // 最新状态事件：只保留最新 time_sync
    timeSyncMu      sync.Mutex
    latestTimeSync  *wsevent.Event
    timeSyncUpdated time.Time
}
```

队列容量建议：

```go
const (
    highBufSize = 16
    sendBufSize = 64
)
```

理由：

- `high` 队列承载低频重要事件，容量不用大。
- `send` 保持现有 64，避免改变普通 bid fanout 的承载行为。
- `time_sync` 不需要容量，latest slot 只有 0/1 个事件。

### 1.3 消息分类器

新增一个 hub 内部函数，集中定义策略：

```go
type eventLane string

const (
    laneHigh   eventLane = "high"
    laneLatest eventLane = "latest"
    laneNormal eventLane = "normal"
)

func classifyEventLane(eventType string) eventLane {
    switch eventType {
    case "time_sync":
        return laneLatest
    case "user_outbid",
        "auction_extended",
        "auction_ended",
        "auction_cancelled",
        "order_created",
        "auction_started",
        "auction_snapshot":
        return laneHigh
    default:
        return laneNormal
    }
}
```

不在 hub 包里 import `internal/app/item/dto`，避免 ws/hub 反向依赖业务模块。这里使用字符串常量，并通过测试锁住分类结果。

### 1.4 写循环优先级

`StartWriteLoop` 的优先级顺序：

```text
1. high 队列
2. latest time_sync
3. normal send 队列
4. websocket control ping
```

解释：

- 高优先级业务事件要比心跳状态更重要，例如成交、取消、被超越。
- `time_sync` 比普通 `bid_success` 更需要稳定节拍，本计划要求线上 P95 < 1s、P99 < 1.5s。
- `bid_success` 仍然按 normal FIFO 写出。
- 如果同一秒产生多个 `time_sync`，只写最新值。

一个可实现的循环结构：

```go
for {
    if event, ok := c.popHigh(); ok {
        if !c.writeTracked(event, int64(len(c.high)), int64(cap(c.high)), "high") {
            return
        }
        continue
    }
    if event, lag, ok := c.popTimeSync(); ok {
        recordTimeSyncWrite(lag)
        if !c.writeTracked(event, 0, 1, "latest") {
            return
        }
        continue
    }

    select {
    case event, ok := <-c.high:
        if !ok { return }
        if !c.writeTracked(event, int64(len(c.high)), int64(cap(c.high)), "high") {
            return
        }
    case event, ok := <-c.send:
        if !ok { return }
        if high, ok := c.popHigh(); ok {
            c.requeueNormalFrontOrBack(event)
            if !c.writeTracked(high, int64(len(c.high)), int64(cap(c.high)), "high") {
                return
            }
            continue
        }
        if latest, lag, ok := c.popTimeSync(); ok {
            c.requeueNormalFrontOrBack(event)
            recordTimeSyncWrite(lag)
            if !c.writeTracked(latest, 0, 1, "latest") {
                return
            }
            continue
        }
        if !c.writeTracked(event, int64(len(c.send)), int64(cap(c.send)), "normal") {
            return
        }
    case <-ticker.C:
        writeControlPing()
    }
}
```

实现时可以写得更简洁，但必须满足：

- pending high 存在时，先写 high。
- pending `time_sync` 存在且没有 high 时，先写 `time_sync`。
- normal 事件不能因为插队逻辑丢失。

为了避免“从 normal channel 取出后又插队导致事件丢失”，推荐增加一个很小的 `deferredNormal *wsevent.Event` 临时槽，而不是把事件重新塞回 channel。

---

## 2. 实现任务

## 任务 1：建立消息分类器

**文件：**
- 修改：`internal/app/ws/hub/conn.go`
- 修改：`internal/app/ws/hub/hub_test.go`

- [ ] **步骤 1：写分类器失败测试**

在 `hub_test.go` 增加：

```go
func TestClassifyEventLane(t *testing.T) {
    tests := []struct {
        eventType string
        want      eventLane
    }{
        {eventType: "time_sync", want: laneLatest},
        {eventType: "user_outbid", want: laneHigh},
        {eventType: "auction_extended", want: laneHigh},
        {eventType: "auction_ended", want: laneHigh},
        {eventType: "auction_cancelled", want: laneHigh},
        {eventType: "order_created", want: laneHigh},
        {eventType: "auction_started", want: laneHigh},
        {eventType: "auction_snapshot", want: laneHigh},
        {eventType: "bid_success", want: laneNormal},
        {eventType: "unknown_event", want: laneNormal},
    }

    for _, tt := range tests {
        if got := classifyEventLane(tt.eventType); got != tt.want {
            t.Fatalf("classifyEventLane(%q) = %q, want %q", tt.eventType, got, tt.want)
        }
    }
}
```

- [ ] **步骤 2：确认 RED**

运行：

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run TestClassifyEventLane -count=1
```

预期：编译失败，因为 `eventLane` 和 `classifyEventLane` 不存在。

- [ ] **步骤 3：实现分类器**

在 `conn.go` 或新文件 `event_lane.go` 增加：

```go
type eventLane string

const (
    laneHigh   eventLane = "high"
    laneLatest eventLane = "latest"
    laneNormal eventLane = "normal"
)

func classifyEventLane(eventType string) eventLane {
    switch eventType {
    case "time_sync":
        return laneLatest
    case "user_outbid", "auction_extended", "auction_ended", "auction_cancelled", "order_created", "auction_started", "auction_snapshot":
        return laneHigh
    default:
        return laneNormal
    }
}
```

- [ ] **步骤 4：确认 GREEN**

运行：

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run TestClassifyEventLane -count=1
```

预期：测试通过。

---

## 任务 2：实现 high / latest / normal 三条投递路径

**文件：**
- 修改：`internal/app/ws/hub/conn.go`
- 修改：`internal/app/ws/hub/hub.go`
- 修改：`internal/app/ws/hub/hub_test.go`

- [ ] **步骤 1：扩展 `fakeSocket`，捕获写出顺序**

在 `fakeSocket` 中增加：

```go
writes []wsevent.Event
```

修改 `WriteJSON`：

```go
func (s *fakeSocket) WriteJSON(v any) error {
    s.mu.Lock()
    err := s.writeJSONErr
    ch := s.writeJSONCh
    if event, ok := v.(wsevent.Event); ok {
        s.writes = append(s.writes, event)
    }
    s.mu.Unlock()
    if ch != nil {
        close(ch)
    }
    return err
}
```

增加 helper：

```go
func (s *fakeSocket) writtenEvents() []wsevent.Event {
    s.mu.Lock()
    defer s.mu.Unlock()
    return append([]wsevent.Event(nil), s.writes...)
}
```

- [ ] **步骤 2：写 high 优先于 normal 的失败测试**

```go
func TestHighPriorityEventWritesBeforeNormalQueue(t *testing.T) {
    h := NewHub(nil)
    ws := newFakeSocket()
    conn := NewConn("conn_1", "user_1", "room_1", ws, h)
    h.Register(conn)

    for i := 0; i < 8; i++ {
        h.SendToRoom("room_1", wsevent.Event{Type: "bid_success", Payload: map[string]any{"seq": i}})
    }
    h.SendToRoom("room_1", wsevent.Event{Type: "auction_ended", Payload: map[string]any{"seq": 99}})

    go conn.StartWriteLoop()
    t.Cleanup(conn.close)

    waitFor(t, func() bool { return len(ws.writtenEvents()) >= 1 })
    first := ws.writtenEvents()[0]
    if first.Type != "auction_ended" {
        t.Fatalf("expected high priority event first, got %+v", first)
    }
}
```

- [ ] **步骤 3：写 latest `time_sync` 覆盖的失败测试**

```go
func TestTimeSyncDeliveryKeepsOnlyLatestEvent(t *testing.T) {
    h := NewHub(nil)
    ws := newFakeSocket()
    conn := NewConn("conn_1", "user_1", "room_1", ws, h)
    h.Register(conn)

    h.SendToRoom("room_1", wsevent.Event{Type: "time_sync", Payload: map[string]any{"seq": 1}})
    h.SendToRoom("room_1", wsevent.Event{Type: "time_sync", Payload: map[string]any{"seq": 2}})

    go conn.StartWriteLoop()
    t.Cleanup(conn.close)

    waitFor(t, func() bool { return len(ws.writtenEvents()) >= 1 })
    first := ws.writtenEvents()[0]
    if first.Type != "time_sync" {
        t.Fatalf("expected time_sync first, got %+v", first)
    }
    payload := first.Payload.(map[string]any)
    if payload["seq"] != 2 {
        t.Fatalf("expected latest time_sync seq=2, got %#v", payload)
    }
}
```

- [ ] **步骤 4：写 high 高于 latest 的失败测试**

```go
func TestHighPriorityEventWritesBeforeTimeSync(t *testing.T) {
    h := NewHub(nil)
    ws := newFakeSocket()
    conn := NewConn("conn_1", "user_1", "room_1", ws, h)
    h.Register(conn)

    h.SendToRoom("room_1", wsevent.Event{Type: "time_sync", Payload: map[string]any{"seq": 1}})
    h.SendToRoom("room_1", wsevent.Event{Type: "auction_cancelled", Payload: map[string]any{"seq": 2}})

    go conn.StartWriteLoop()
    t.Cleanup(conn.close)

    waitFor(t, func() bool { return len(ws.writtenEvents()) >= 1 })
    first := ws.writtenEvents()[0]
    if first.Type != "auction_cancelled" {
        t.Fatalf("expected high priority event before time_sync, got %+v", first)
    }
}
```

- [ ] **步骤 5：确认 RED**

运行：

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run 'TestHighPriorityEventWritesBeforeNormalQueue|TestTimeSyncDeliveryKeepsOnlyLatestEvent|TestHighPriorityEventWritesBeforeTimeSync' -count=1
```

预期：至少前两个失败，因为当前所有事件都走普通 FIFO。

- [ ] **步骤 6：修改 `Conn` 结构**

把 `Conn` 调整为：

```go
type Conn struct {
    id     string
    userID string
    roomID string
    ws     socket
    high   chan wsevent.Event
    send   chan wsevent.Event
    hub    *Hub

    timeSyncMu      sync.Mutex
    latestTimeSync  *wsevent.Event
    timeSyncUpdated time.Time

    closeMu   sync.RWMutex
    closeOnce sync.Once
    closed    bool
}
```

`NewConn` 初始化：

```go
high: make(chan wsevent.Event, highBufSize),
send: make(chan wsevent.Event, sendBufSize),
```

`newTestConn` 也要补 `high: make(chan wsevent.Event, 8)`。

- [ ] **步骤 7：增加三个 enqueue 方法**

```go
func (c *Conn) enqueueHigh(event wsevent.Event) bool {
    c.closeMu.RLock()
    defer c.closeMu.RUnlock()
    if c.closed {
        return false
    }
    select {
    case c.high <- event:
        return true
    default:
        return false
    }
}

func (c *Conn) enqueueNormal(event wsevent.Event) bool {
    c.closeMu.RLock()
    defer c.closeMu.RUnlock()
    if c.closed {
        return false
    }
    select {
    case c.send <- event:
        return true
    default:
        return false
    }
}

func (c *Conn) enqueueTimeSync(event wsevent.Event) (overwritten bool, ok bool) {
    c.closeMu.RLock()
    defer c.closeMu.RUnlock()
    if c.closed {
        return false, false
    }
    c.timeSyncMu.Lock()
    defer c.timeSyncMu.Unlock()
    overwritten = c.latestTimeSync != nil
    copy := event
    c.latestTimeSync = &copy
    c.timeSyncUpdated = time.Now()
    return overwritten, true
}
```

保留兼容方法：

```go
func (c *Conn) enqueue(event wsevent.Event) bool {
    switch classifyEventLane(event.Type) {
    case laneHigh:
        return c.enqueueHigh(event)
    case laneLatest:
        _, ok := c.enqueueTimeSync(event)
        return ok
    default:
        return c.enqueueNormal(event)
    }
}
```

- [ ] **步骤 8：修改 `Hub.deliver` 路由**

`deliver` 根据 `classifyEventLane(event.Type)` 调用对应 enqueue，并记录不同 lane 的 `WSDeliveryMetric`：

```go
lane := classifyEventLane(event.Type)
switch lane {
case laneHigh:
    queueLen := int64(len(c.high))
    queueCap := int64(cap(c.high))
    ok := c.enqueueHigh(event)
    recordDelivery(ok, event.Type, "high", queueLen, queueCap)
    return ok
case laneLatest:
    overwritten, ok := c.enqueueTimeSync(event)
    reason := "none"
    if overwritten {
        reason = "overwrite"
    }
    recordDelivery(ok, event.Type, reason, 1, 1)
    return ok
default:
    queueLen := int64(len(c.send))
    queueCap := int64(cap(c.send))
    ok := c.enqueueNormal(event)
    recordDelivery(ok, event.Type, "normal", queueLen, queueCap)
    return ok
}
```

实现时可抽 `recordDelivery` helper；失败时保持当前 close 逻辑。

- [ ] **步骤 9：修改写循环优先级**

写循环顺序必须是：

```text
high -> time_sync latest -> normal
```

推荐实现：

```go
func (c *Conn) nextEvent() (wsevent.Event, eventLane, time.Duration, bool) {
    select {
    case event, ok := <-c.high:
        return event, laneHigh, 0, ok
    default:
    }
    if event, lag, ok := c.popTimeSync(); ok {
        return event, laneLatest, lag, true
    }
    select {
    case event, ok := <-c.high:
        return event, laneHigh, 0, ok
    case event, ok := <-c.send:
        if !ok {
            return wsevent.Event{}, laneNormal, 0, false
        }
        return event, laneNormal, 0, true
    }
}
```

在 `StartWriteLoop` 中优先处理 `nextEvent()`，并保留 ticker 分支。实现时注意：不能让 `nextEvent` 永久阻塞导致 control ping 永远不执行；可以在 select 中保留 ticker，并在每轮 select 前先 drain high/latest。

- [ ] **步骤 10：确认 GREEN**

运行：

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run 'TestHighPriorityEventWritesBeforeNormalQueue|TestTimeSyncDeliveryKeepsOnlyLatestEvent|TestHighPriorityEventWritesBeforeTimeSync|TestStartWriteLoopRecordsSocketWriteMetrics|TestFullChannelRecordsDroppedDeliveryMetric' -count=1
```

预期：全部通过。

---

## 任务 3：补充与分级策略相关的指标

**文件：**
- 修改：`internal/core/observability/metrics.go`
- 修改：`internal/core/observability/provider.go`
- 修改：`internal/core/observability/provider_test.go`
- 修改：`internal/app/ws/hub/hub_test.go`

这部分是服务于设计验证，不是主设计本身。

- [ ] **步骤 1：增加 `WSTimeSyncMetric`**

```go
type WSTimeSyncMetric struct {
    Action   string
    Result   string
    WriteLag time.Duration
}
```

Recorder 增加：

```go
WSTimeSync(context.Context, WSTimeSyncMetric)
```

OTel 指标：

```text
ws.time_sync.count{action,result}
ws.time_sync.write_lag.duration{action,result}
```

- [ ] **步骤 2：记录 overwrite 和 write lag**

在 `enqueueTimeSync` 覆盖旧值时记录：

```go
WSTimeSyncMetric{Action: "overwrite", Result: "success"}
```

在写出 `time_sync` 时记录：

```go
WSTimeSyncMetric{Action: "write", Result: "success", WriteLag: lag}
```

写失败时记录：

```go
WSTimeSyncMetric{Action: "write", Result: "failed", WriteLag: lag}
```

- [ ] **步骤 3：指标测试**

在 `hub_test.go` 增加：

```go
func TestTimeSyncOverwriteRecordsMetric(t *testing.T) {
    rec := &captureWSRecorder{}
    observability.SetDefaultRecorder(rec)
    t.Cleanup(func() { observability.SetDefaultRecorder(nil) })

    h := NewHub(nil)
    c := newTestConn("user_1", "room_1")
    h.Register(c)

    h.SendToRoom("room_1", wsevent.Event{Type: "time_sync"})
    h.SendToRoom("room_1", wsevent.Event{Type: "time_sync"})

    if got := rec.timeSyncCount("overwrite"); got != 1 {
        t.Fatalf("expected one overwrite metric, got %d", got)
    }
}
```

- [ ] **步骤 4：duration bucket 配置**

`provider.go` 增加：

```go
"ws.time_sync.write_lag.duration",
```

`provider_test.go` 的 bucket 测试加入同名指标。

- [ ] **步骤 5：运行测试**

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/core/observability ./internal/app/ws/hub -count=1
```

预期：通过。

---

## 任务 4：更新性能 runner 查询

**文件：**
- 修改：`docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
- 修改：`docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go`

- [ ] **步骤 1：runner 查询加入新指标**

新增 Prometheus 查询：

```go
{
    Name:  "ws_time_sync_overwrite_rps",
    Query: `sum(rate(ws_time_sync_count_total{action="overwrite"}[1m]))`,
},
{
    Name:  "ws_time_sync_write_lag_p95",
    Query: `histogram_quantile(0.95, sum(rate(ws_time_sync_write_lag_duration_bucket{action="write"}[1m])) by (le))`,
},
{
    Name:  "ws_delivery_by_event_lane",
    Query: `sum(rate(ws_delivery_count_total[1m])) by (event_type, result, reason)`,
},
```

- [ ] **步骤 2：runner 测试断言**

`TestDefaultPrometheusQueriesUseObservedMetricNames` 增加：

```go
"ws_time_sync_count_total",
"ws_time_sync_write_lag_duration_bucket",
```

- [ ] **步骤 3：运行测试**

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

预期：通过。

---

## 任务 5：本地验证门槛

运行：

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/core/observability ./internal/app/ws/hub ./internal/app/item/service ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

预期：全部通过。

检查 diff：

```bash
rtk git diff -- internal/app/ws/hub/conn.go internal/app/ws/hub/hub.go internal/app/ws/hub/hub_test.go internal/core/observability/metrics.go internal/core/observability/provider.go internal/core/observability/provider_test.go docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go
```

检查项：

- `time_sync` 只进 latest slot，不进 normal FIFO。
- high 事件进入 high 队列。
- `bid_success` 继续走 normal FIFO。
- high 优先级高于 latest，latest 优先级高于 normal。
- normal 事件不会因为插队逻辑丢失。
- 指标不包含 room ID、item ID、user ID、connection ID 等高基数字段。

---

## 任务 6：部署后的线上验证

本计划不主动部署。用户部署后执行验证。

### 6.1 preflight

```bash
rtk ssh deploy@115.191.46.148 "kubectl rollout status deployment/live-auction-backend -n live-auction && kubectl get deployment live-auction-backend -n live-auction -o jsonpath='{.spec.template.spec.containers[0].image}{\"\\n\"}' && kubectl get pods -n live-auction -l app=live-auction-backend -o wide"
rtk curl -fsS https://auction-api.kirac0on.com/health
```

### 6.2 压测

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_perf_auction_20260604_ws_message_lane_probe \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL=https://auction-api.kirac0on.com \
  PERF_PROMETHEUS_URL=http://127.0.0.1:19090 \
  PERF_OBSERVABILITY_STEP=30s \
  PERF_STOP_FILE=docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/STOP_ws_message_lane_probe \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  PERF_REQUEST_MIX=core_bid_80_ranking_10_item_10 \
  PERF_REQUEST_TIMEOUT=15s \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=760 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

### 6.3 验收

- `time_sync` P95 < 1s。
- `time_sync` P99 < 1.5s。
- `ws_time_sync_write_lag_p95` < 1s。
- `ws_delivery dropped = 0`。
- `ws_write_p95 < 2ms`。
- backend restart = 0。
- high priority 事件没有 dropped。

### 6.4 报告

创建：

```text
docs/agent-testing/reports/<timestamp>-auction-ws-message-lane-priority.md
```

报告必须包含：

- 批次 ID。
- 后端镜像。
- 阶段指标。
- `time_sync` P95/P99 优化前后对比。
- `ws_time_sync_overwrite_rps`。
- `ws_time_sync_write_lag_p95`。
- `ws_delivery_by_event_lane`。
- cleanup 结果。
- 多实例是否仍暂缓。

---

## 验收标准

- 本地测试通过：

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/core/observability ./internal/app/ws/hub ./internal/app/item/service ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

- 线上 300 WS + 70 QPS 验证满足：
  - `time_sync` P95 < 1s。
  - `time_sync` P99 < 1.5s。
  - `ws_time_sync_write_lag_p95` < 1s。
  - `ws_delivery dropped = 0`。
  - `ws_write_p95 < 2ms`。
  - backend restart = 0。

- 不做多实例部署改动。

---

## 自检

- 设计主线是消息三分类：high / latest / normal。
- `time_sync` 优化不是靠补指标，而是靠 latest slot + 优先写出。
- 高优先级业务事件不会被 `bid_success` 阻塞。
- `bid_success` 继续走普通队列，保持现有 coalescing 和 fanout 行为。
- 指标只用于验证分级策略是否生效。
- 多实例明确不在本计划范围内。
