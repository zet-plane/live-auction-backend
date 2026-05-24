# 房间模块测试说明

## 1. 模块目标

房间模块负责管理直播间的生命周期。每个商家有且只有一个直播间，状态在 `idle`（未开播）和 `live`（直播中）之间切换。Redis 缓存维护房间的实时状态（在线人数、当前商品 ID）以及待拍商品队列（由商品模块写入的 ZSET）。读路径降级静默，写路径软失败，MySQL 是唯一真相来源。

## 2. 测试边界

Agent 可以测试：

- 公开房间列表接口 `GET /api/v1/rooms`。
- 公开房间详情接口 `GET /api/v1/rooms/{room_id}`。
- 商家激活/获取直播间接口 `POST /api/v1/merchant/room`。
- 商家查看自己直播间接口 `GET /api/v1/merchant/room`。
- 商家开播接口 `POST /api/v1/rooms/{room_id}/start`。
- 商家下播接口 `POST /api/v1/rooms/{room_id}/end`。
- `ActivateRoom`、`GetMerchantRoom`、`StartRoom`、`EndRoom`、`GetRoom`、`ListRooms` Service 逻辑。
- `LiveRoom` 模型字段、唯一索引约束、软删除行为和数据库写入。
- Redis key `auction:room:{room_id}:state` 的读写行为（HSET/HGETALL）。
- Redis key `auction:room:{room_id}:item_queue` 的读取行为（ZRANGE，由商品模块写入）。
- 房间状态、Redis 在线人数富化、待拍队列富化、统一响应结构和错误返回。
- `MerchantRoomDTO` 的 `status_text`、`queued_count`、`actions` 派生字段。

## 3. 禁止事项

- 不测试出价、竞拍规则、商品详情或订单相关业务。
- 不调用真实第三方服务。
- 不直接清空数据库或删除 Redis key（测试数据使用带批次前缀的 ID）。
- 不修改生产配置或复用线上真实房间数据。
- 不绕过业务接口直接修改房间状态，除非文档明确用于故障注入。
- 不自行创造文档和代码中都没有定义的房间状态（如 `ended`、`suspended`）。
- 本地单元测试不允许直接连接数据库或 Redis，必须使用 fake store 和 fake cache。
- Agent 连接线上或线上等价数据库时，只能操作本次测试创建的数据或带测试批次 ID 的数据。
- 不直接向 `auction:room:{room_id}:item_queue` 写入数据来伪造商品队列；集成测试中若需要队列数据，应通过商品模块的上架接口触发。

## 4. 业务规则

- 商家身份才能激活直播间、开播、下播和查看自己的直播间。
- 普通用户或未登录用户可以公开查询房间详情和房间列表，但不能执行写操作。
- 每个商家有且只有一个直播间（`MerchantID` 唯一索引）。
- `ActivateRoom` 是幂等接口：商家已有直播间时直接返回现有房间（不更新 title），没有时创建。
- 创建直播间时 `title` 会去除首尾空格，不能为空。
- 直播间 ID 使用 `room_` 前缀。
- 新建直播间初始状态为 `idle`。
- `StartRoom` 只允许 `idle -> live`，非 `idle` 状态返回 `ErrInvalidRequest`。
- `EndRoom` 只允许 `live -> idle`，非 `live` 状态返回 `ErrInvalidRequest`。
- `StartRoom` 成功后软失败写入 Redis state（HSET: merchant_id, status="live", current_item_id, online_count=0）。
- `EndRoom` 成功后软失败更新 Redis state 的 status 字段为 "idle"。
- `GetRoom` 和 `ListRooms` 从 Redis 读取 `online_count` 和 `item_queue`；Redis miss 时降级返回 0 和空数组，不报错。
- `GetMerchantRoom` 同样从 Redis 读取 `online_count` 和 `queued_count`（队列长度）。
- `ListRooms` 不传 `status` 时默认只返回 `live` 状态的房间。
- `findMerchantRoom` 验证商家归属时若不属于当前商家，返回 `ErrNotFound`（不暴露他人房间是否存在）。
- `item_queue` 在 `RoomDetailDTO` 中永远不为 nil，最少返回空数组。

## 5. 业务不变量

- 每个商家只能有一个直播间，不能创建第二个。
- 直播间状态只能在 `idle` 和 `live` 之间流转，不能跳过或进入未定义状态。
- `idle` 状态下不能执行开播（不能 `idle -> idle` 或跳到其他状态）。
- `live` 状态下不能再次开播。
- `idle` 状态下不能执行下播。
- MySQL 是房间状态的唯一真相来源；Redis 状态滞后或缺失时，接口响应仍应基于 MySQL 状态。
- 非商家身份无法创建或管理直播间。
- 非所属商家无法开播或下播其他商家的直播间。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 6. 测试数据准备

需要准备：

- 至少 1 个普通用户账号和有效 token。
- 至少 2 个商家账号，用于验证房间归属隔离。
- 至少 2 个商家 token（分属不同商家）。
- 至少 1 个无效 token。
- 至少 1 个合法创建房间请求（title 合法）。
- 至少 1 个非法创建房间请求（title 为空、超长）。
- 至少 1 个带测试批次 ID 的房间 title 前缀，例如 `agent_room_<batch>_jade`。
- 接口契约和集成测试需要：已激活直播间的商家（idle 状态）；已开播的房间（live 状态）；用于验证隔离的第二个商家房间。

## 7. 依赖策略建议

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | fake store + fake cache；禁止直连数据库和 Redis | 稳定验证 Service 业务规则、状态流转和 Redis 降级逻辑 |
| Agent 接口契约测试 | 真实 handler 或本地服务；允许连接测试数据库；通过用户模块获取 token | 验证真实请求绑定、鉴权中间件、响应结构和错误码 |
| Agent 模块集成测试 | 真实 GORM store + 真实 Redis；允许连接测试数据库 | 验证 MySQL 唯一约束、Redis key 写入结构和 ZSET 读取 |
| Agent 并发测试 | 真实数据库事务 + 真实 HTTP 并发请求 | mock 会让并发结论失效 |
| 状态一致性测试 | 对比 HTTP 响应、live_rooms 表、Redis HSET | 验证接口返回和持久化状态一致 |

## 8. 单元测试

当前已有 11 个单元测试覆盖核心路径（`service_test.go`），需要覆盖（含已有）：

- 普通用户激活直播间返回未授权。
- 商家首次激活直播间成功，创建 `idle` 状态房间，ID 含 `room_` 前缀。
- 商家再次激活直播间（幂等），返回同一房间 ID，store 只有 1 条记录。
- 激活已有直播间时返回的 `queued_count` 来自 Redis item_queue 长度。
- `StartRoom` 将 `idle` 房间切换到 `live`，store 状态为 `live`。
- `StartRoom` 对 `live` 状态房间返回 `ErrInvalidRequest`。
- `StartRoom` 成功后 Redis state 写入 status="live"。
- `EndRoom` 将 `live` 房间切换到 `idle`，store 状态为 `idle`。
- `EndRoom` 对 `idle` 状态房间返回 `ErrInvalidRequest`。
- `EndRoom` 成功后 Redis state 更新 status="idle"。
- `GetRoom` 从 Redis 读取 `online_count` 富化到响应。
- `GetRoom` 从 Redis 读取 `item_queue` 富化到响应。
- `GetRoom` Redis miss 时降级返回 `online_count=0`、`item_queue=[]`，不报错。
- 非商家调用 `StartRoom`/`EndRoom` 返回未授权。
- 非所属商家调用 `StartRoom`/`EndRoom` 返回 `ErrNotFound`。
- `ListRooms` 不传 status 时默认返回 `live` 状态房间。
- `ListRooms` 传入 `status=idle` 只返回 `idle` 状态房间。
- `GetMerchantRoom` 非商家返回未授权。
- `GetMerchantRoom` 商家无房间时返回 not found。

## 9. 接口契约测试

需要覆盖：

- `POST /api/v1/merchant/room` 未登录、普通用户 token、无效 token 均返回未授权。
- `POST /api/v1/merchant/room` 商家首次激活成功，响应含 `data.id`、`data.status`、`data.actions`。
- `POST /api/v1/merchant/room` 商家重复激活返回同一 `data.id`（幂等）。
- `POST /api/v1/merchant/room` 缺少 `title` 或 title 为空字符串返回参数错误。
- `POST /api/v1/merchant/room` title 超过 128 字符返回参数错误。
- `GET /api/v1/merchant/room` 未登录或普通用户返回未授权。
- `GET /api/v1/merchant/room` 商家无直播间返回 not found。
- `GET /api/v1/merchant/room` 商家有直播间返回 `MerchantRoomDTO` 完整结构（id、title、status、status_text、online_count、queued_count、actions、created_at、updated_at）。
- `GET /api/v1/merchant/room` idle 状态下 `actions.can_start=true`、`actions.can_end=false`。
- `GET /api/v1/merchant/room` live 状态下 `actions.can_start=false`、`actions.can_end=true`。
- `POST /api/v1/rooms/{room_id}/start` 未登录、普通用户返回未授权。
- `POST /api/v1/rooms/{room_id}/start` 对 idle 房间成功响应 `data=null`。
- `POST /api/v1/rooms/{room_id}/start` 对 live 房间返回业务错误。
- `POST /api/v1/rooms/{room_id}/start` 非所属商家操作返回 not found。
- `POST /api/v1/rooms/{room_id}/end` 未登录、普通用户返回未授权。
- `POST /api/v1/rooms/{room_id}/end` 对 live 房间成功响应 `data=null`。
- `POST /api/v1/rooms/{room_id}/end` 对 idle 房间返回业务错误。
- `POST /api/v1/rooms/{room_id}/end` 非所属商家操作返回 not found。
- `GET /api/v1/rooms` 无需登录，成功响应 `data` 为数组。
- `GET /api/v1/rooms` 不传 status 时只返回 `live` 状态房间。
- `GET /api/v1/rooms` 传 `status=idle` 时只返回 `idle` 状态房间。
- `GET /api/v1/rooms/{room_id}` 无需登录，成功响应含 `item_queue` 字段（数组，可空）。
- `GET /api/v1/rooms/{room_id}` 查询不存在房间返回 not found。

## 10. 业务场景测试

### 商家激活并开播直播间

Given:
- 商家账号已创建，持有有效 token。

When:
- 调用 `POST /api/v1/merchant/room`（title="直播间"）激活直播间。
- 使用返回的 `id` 调用 `POST /api/v1/rooms/{room_id}/start`。
- 调用 `GET /api/v1/rooms/{room_id}` 查询公开详情。
- 调用 `GET /api/v1/merchant/room` 查询商家视图。

Then:
- 激活接口返回 `id`（含 `room_` 前缀）、`status=idle`、`actions.can_start=true`。
- 开播接口返回成功。
- 公开详情 `status=live`，`online_count` 为数字（0 或 Redis 中的值）。
- 商家视图 `status=live`、`status_text="直播中"`、`actions.can_start=false`、`actions.can_end=true`。
- 数据库 `live_rooms` 中该记录 `status=live`。
- Redis `auction:room:{room_id}:state` 存在，`status` 字段为 `live`。

### 商家下播直播间

Given:
- 商家直播间处于 `live` 状态。

When:
- 调用 `POST /api/v1/rooms/{room_id}/end`。
- 调用 `GET /api/v1/merchant/room`。

Then:
- 下播接口返回成功。
- 商家视图 `status=idle`、`status_text="未开播"`、`actions.can_start=true`、`actions.can_end=false`。
- 数据库状态为 `idle`。
- Redis `auction:room:{room_id}:state` 的 `status` 字段更新为 `idle`。

### 激活直播间幂等

Given:
- 商家已有 `idle` 状态直播间（id=room_A）。

When:
- 再次调用 `POST /api/v1/merchant/room`（title 不同）。

Then:
- 返回 `id=room_A`（同一房间）。
- 数据库只有 1 条该商家的直播间记录。
- title 未被覆盖（返回原 title）。

### 直播间归属隔离

Given:
- 商家 A 和商家 B 各自激活了直播间。

When:
- 商家 B 调用 `POST /api/v1/rooms/{merchant_A_room_id}/start`。
- 商家 B 调用 `POST /api/v1/rooms/{merchant_A_room_id}/end`。

Then:
- 两次请求均返回 not found。
- 商家 A 的直播间状态没有发生变化。

### 公开列表只展示直播中房间

Given:
- 商家 A 的直播间为 `live`，商家 B 的直播间为 `idle`。

When:
- 不带参数调用 `GET /api/v1/rooms`。

Then:
- 响应列表只包含 `live` 状态房间（商家 A 的房间）。
- 商家 B 的 `idle` 房间不出现在列表中。

### GetRoom 含商品队列富化

Given:
- 直播间已存在，Redis `item_queue` ZSET 中有 2 个 item_id（由商品模块上架时写入）。

When:
- 调用 `GET /api/v1/rooms/{room_id}`。

Then:
- 响应 `item_queue` 包含这 2 个 item_id，顺序为 ZSET 升序（score 从低到高）。

## 11. 异常测试

需要覆盖：

- 未登录激活直播间。
- 普通用户激活直播间。
- token 无效或过期时激活直播间。
- 激活时缺少 `title` 字段。
- 激活时 `title` 为空字符串。
- 激活时 `title` 超过 128 字符。
- `StartRoom` 对已 `live` 的房间再次开播。
- `EndRoom` 对 `idle` 的房间执行下播。
- `StartRoom`/`EndRoom` 传入不存在的 `room_id`。
- 非所属商家执行 `StartRoom`/`EndRoom`（room_id 属于其他商家）。
- 普通用户执行 `StartRoom`/`EndRoom`。
- `GET /api/v1/merchant/room` 商家尚未激活直播间。
- `GET /api/v1/rooms/{room_id}` 不存在的 room_id。
- `room_id` 带首尾空格时（Service 层 `strings.TrimSpace` 处理，接口层按 flamego 路由参数行为）。

## 12. 边界测试

需要覆盖：

- `title` 长度刚好 1。
- `title` 长度刚好 128。
- `title` 长度为 129。
- `title` 包含首尾空格时，返回的 `title` 已去除空格。
- `title` 全为空格（`strings.TrimSpace` 后为空）时的 Service 层行为（当前实现未对空 title 做 Service 层校验，只有绑定层 `min=1` 防守）。
- `ListRooms` 传入未知 `status`（非 idle/live）时的行为（当前 DAO 直接按字符串过滤，返回空列表）。
- `GetRoom` 在 Redis 完全不可用时的降级行为。
- `online_count` 在 Redis 有值（>0）和无值（miss）时的响应差异。
- `item_queue` 为空时响应为 `[]`（非 `null`）。

## 13. 并发测试

需要覆盖：

- 同一商家并发调用 `ActivateRoom` 多次，只能创建 1 条直播间记录（数据库唯一约束保证）。
- 同一直播间并发执行 `StartRoom`，最终状态为 `live`，不能出现状态不一致。
- 同一直播间并发执行 `EndRoom`，最终状态为 `idle`，不能出现状态不一致。
- 商家 A 和商家 B 并发操作各自直播间，互不影响。

验证要求：

- 并发 `ActivateRoom` 后 `live_rooms` 表中该商家只有 1 条记录。
- 并发状态动作后 MySQL 状态和后续查询接口状态一致。
- 不能因并发导致房间进入未定义状态。

注意：当前状态流转使用先查后保存，并发重复状态动作可能都返回成功或出现最后写入覆盖；如需严格单成功语义，需用户确认预期。

## 14. 状态一致性测试

需要验证：

- 激活接口返回的 `id` 与数据库 `live_rooms.id` 一致。
- `status` 字段在 HTTP 响应、数据库记录、Redis state 三者之间一致。
- 开播后公开详情 `status=live` 与数据库和 Redis 一致。
- 下播后公开详情 `status=idle` 与数据库和 Redis 一致。
- `online_count` 来自 Redis 的实际值，Redis miss 时为 0。
- `item_queue` 内容与 Redis `auction:room:{room_id}:item_queue` ZSET 的升序成员一致。
- `queued_count` 等于 `item_queue` 的长度。
- `actions.can_start` 等于 `status == idle`；`actions.can_end` 等于 `status == live`。
- `status_text` 与 `status` 对应（idle→"未开播"，live→"直播中"）。

状态不一致时，agent 必须记录：

- 哪两个数据源不一致。
- 哪个接口或步骤触发不一致。
- 不一致是否影响商家开播、下播或用户观看。

## 15. WebSocket 测试

当前房间模块实现不适用。

`StartRoom` 和 `EndRoom` 仅更新 MySQL 和 Redis 状态，未实现 WebSocket 广播。若需要验证直播间开播/下播时向用户推送事件，应在后续实时广播模块或跨模块流程文档中定义。

## 16. 回归测试

以下问题一旦出现，必须沉淀为回归测试：

- 同一商家激活两次创建了两个不同 ID 的直播间。
- 普通用户或未登录用户可以激活、开播或下播直播间。
- 非所属商家可以操作其他商家的直播间。
- 开播成功但数据库状态仍为 `idle`。
- 下播成功但数据库状态仍为 `live`。
- `idle` 房间重复开播返回成功（应返回错误）。
- `live` 房间重复下播返回成功（应返回错误）。
- Redis miss 时 `GetRoom` 报错而非降级。
- `item_queue` 为 `null` 而非空数组。
- `ListRooms` 不传 status 时返回了 `idle` 房间。
- 公开接口（`GetRoom`、`ListRooms`）要求登录才能访问。
- 激活接口在商家已有房间时创建了新房间（幂等失败）。

## 17. 通过标准

**核心验证点（全部通过才算过）：**

- 激活直播间接口响应结构符合统一响应格式，id 含 `room_` 前缀，状态为 `idle`，有 HTTP 响应作为证据。
- 商家幂等激活不重复创建房间，有 store 断言或数据库查询作为证据。
- 商家权限和房间归属隔离成立，非所属商家操作返回 not found，有接口响应或单元测试作为证据。
- 状态流转只能 `idle ↔ live`，非法状态动作返回错误，有接口响应或单元测试作为证据。
- MySQL 状态与后续查询接口一致，有 HTTP 响应和数据库记录作为证据。
- Redis miss 时 `GetRoom` 降级正常，有单元测试或接口测试作为证据。

**辅助验证点（建议验证，可附说明跳过）：**

- Redis state 在 `StartRoom` 后写入正确字段，在 `EndRoom` 后更新 status 字段。
- `status_text`、`queued_count`、`actions` 派生字段与 status 一致。
- 房间模块自动迁移成功创建或更新 `live_rooms` 表结构。
- `ListRooms` 过滤和 `GetRoom` roomID trim 行为符合预期。
- 错误响应的 `code`、`message` 与 `pkg/errorx` 定义一致。

## 18. 需用户确认的问题

- `ListRooms` 传入未知 status 值时，当前 DAO 直接按字符串过滤返回空列表；是否应返回参数错误？
- `ActivateRoom` 幂等时是否应更新 title 为新传入值？当前实现不更新。
- `StartRoom` Redis 初始化软失败时是否需要记录告警日志；当前代码静默丢弃错误。
- 直播间删除场景（软删除）：当前实现无删除接口，是否需要？
- `item_queue` 排列顺序依赖 Redis ZSET score（由商品上架时的时间戳写入），是否需要文档明确顺序语义？
- 公开 `GET /api/v1/rooms` 接口是否需要分页？当前实现返回全量列表。
- 并发状态动作预期是严格只有一个请求成功，还是允许幂等成功？

## 19. 失败报告格式

测试失败时，agent 必须输出：

```text
失败场景：
复现步骤：
期望结果：
实际结果：
相关证据：
可能原因：
建议修复点：
建议新增的回归测试：
已知缺口：（本次测试因文档或实现原因未覆盖的风险，以及建议如何补充）
```

如果是不变量违反，额外输出：

```text
违反的不变量：
违反位置：
期望状态：
实际状态：
```
