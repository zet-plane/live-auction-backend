# 房间模块测试说明

## 1. 模块目标

房间模块负责管理直播间的生命周期。每个商家有且只有一个直播间，状态在 `idle`（未开播）和 `live`（直播中）之间切换。Redis 缓存维护房间实时状态（在线人数、当前商品 ID）以及待拍商品队列（由商品模块写入的 ZSET）。当前实现中 MySQL 是房间状态的唯一真相来源；Redis 读路径降级静默，写路径软失败。

## 2. 代码定位索引

| 对象 | 代码位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/room/router/room.go` | 注册公开房间接口和鉴权后的商家房间接口 |
| Handler | `internal/app/room/handler/room.go` | 请求绑定、鉴权上下文、路由参数、统一响应 |
| DTO | `internal/app/room/dto/room.go` | 创建请求、公开详情、商家视图、状态文案和 actions |
| Service | `internal/app/room/service/service.go` | 激活幂等、状态流转、商家归属、Redis 富化和降级 |
| DAO | `internal/app/room/dao/room.go` | `Store` 接口、GORM 持久化、按状态列表 |
| Model | `internal/app/room/model/room.go` | `LiveRoom`、`RoomStatus`、唯一索引、软删除字段 |
| Cache | `internal/app/room/cache/cache.go` | Redis room state 和 item queue 读取 |
| 单元测试建议位置 | `internal/app/room/service/*_test.go` | 使用 fake store、fake cache、固定商家身份 |
| Agent 测试契约 | `docs/agent-testing/modules/room.md` | 接口契约、集成、场景和一致性测试边界 |

## 3. 测试边界

Agent 可以测试：

- HTTP 接口：`GET /api/v1/rooms`、`GET /api/v1/rooms/{room_id}`、`POST /api/v1/merchant/room`、`GET /api/v1/merchant/room`、`POST /api/v1/rooms/{room_id}/start`、`POST /api/v1/rooms/{room_id}/end`。
- Service 方法：`ActivateRoom`、`GetMerchantRoom`、`StartRoom`、`EndRoom`、`GetRoom`、`ListRooms`。
- DAO / Model：`LiveRoom` 字段、唯一索引、软删除字段、数据库写入和查询。
- Redis key：`auction:room:{room_id}:state`、`auction:room:{room_id}:item_queue`。
- 房间状态、Redis 在线人数富化、待拍队列富化、统一响应结构、错误返回和 `MerchantRoomDTO` 派生字段。

当前房间模块不直接测试出价、竞拍规则、商品详情、订单、支付或 WebSocket 广播。若要验证待拍队列真实写入，应通过商品模块上架接口触发；若要验证完整竞拍生命周期，应转到跨模块流程文档。

## 4. 禁止事项

- 不测试出价、竞拍规则、商品详情或订单相关完整业务。
- 不调用真实第三方服务。
- 不直接清空数据库或删除非本批次 Redis key。
- 不修改生产配置或复用线上真实房间数据。
- 不绕过业务接口直接修改房间状态，除非文档明确用于故障注入。
- 不自行创造文档和代码中都没有定义的房间状态（如 `ended`、`suspended`）。
- 本地单元测试不允许直接连接数据库、Redis、HTTP 服务、WebSocket 或外部系统，必须使用 mock/fake 数据。
- Agent 连接线上或线上等价数据库/Redis 时，只能操作本次测试创建的数据或带测试批次 ID 的数据。
- 不直接向 `auction:room:{room_id}:item_queue` 写入数据来伪造商品队列；集成测试中若需要队列数据，应通过商品模块的上架接口触发，除非当前测试明确是 Redis 降级/读取故障注入。
- 不在测试报告中写入线上地址、凭据、密码、真实 token 或可复用密钥。

## 5. 测试依赖策略

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | 使用 fake store、fake cache、固定用户身份；禁止直连 MySQL、Redis、HTTP 服务或 WebSocket | 稳定验证 Service 业务规则、状态流转、幂等和 Redis 降级逻辑 |
| Agent 接口契约测试 | 使用真实 handler 或本地服务；使用真实测试数据库和测试 Redis；通过用户模块获取 token | 验证真实请求绑定、鉴权中间件、响应结构和错误码 |
| Agent 模块集成测试 | 使用真实 GORM store、真实测试数据库和真实测试 Redis | 验证 MySQL 唯一约束、Redis key 写入结构和 ZSET 读取 |
| 场景测试 | 使用真实接口链路、真实测试数据库，涉及队列时通过商品模块上架触发 | 验证用户可见业务链路和跨接口状态变化 |
| Agent 并发测试 | 使用真实数据库事务、真实 Redis 和真实 HTTP 并发请求 | mock/fake 无法证明唯一约束和状态动作并发结果 |
| 状态一致性测试 | 对比 HTTP 响应、`live_rooms` 表、Redis room state、Redis item queue | 验证接口返回、持久化状态、缓存状态和派生字段一致 |
| WebSocket 测试 | 房间模块内不适用 | 当前实现没有房间开播/下播 WebSocket 广播 |

## 6. 全局测试数据准备

```text
测试批次 ID：agent_room_<YYYYMMDDHHMMSS>
房间 title 前缀：agent_room_<batch>_
数据只允许操作本批次创建的数据。
测试结束后必须记录数据库软删除/清理结果和 Redis key 清理结果。
```

需要准备：

- 至少 1 个普通用户账号和有效 token。
- 至少 2 个商家账号和对应商家 token，用于验证房间归属隔离。
- 至少 1 个无效 token。
- 至少 1 个合法创建房间请求（`title` 合法）。
- 非法请求体集合：缺少 `title`、`title` 为空、`title` 超长。
- 已激活直播间的商家（`idle` 状态）。
- 已开播的房间（`live` 状态）。
- 用于隔离验证的第二个商家房间。
- 若执行待拍队列测试，应通过商品模块创建并上架本批次商品，触发 Redis `auction:room:{room_id}:item_queue`。

## 7. 业务规则

事实：

- 商家身份才能激活直播间、开播、下播和查看自己的直播间。
- 普通用户或未登录用户可以公开查询房间详情和房间列表，但不能执行写操作。
- 每个商家有且只有一个直播间，`LiveRoom.MerchantID` 使用唯一索引。
- `ActivateRoom` 是幂等接口：商家已有直播间时直接返回现有房间，不更新 title；没有时创建。
- 创建直播间时 `title` 会去除首尾空格，HTTP 绑定层要求长度 1 到 128。
- 直播间 ID 使用 `room_` 前缀。
- 新建直播间初始状态为 `idle`。
- `StartRoom` 只允许 `idle -> live`，非 `idle` 状态返回 `ErrInvalidRequest`。
- `EndRoom` 只允许 `live -> idle`，非 `live` 状态返回 `ErrInvalidRequest`。
- `StartRoom` 成功后先更新 MySQL，再软失败写入 Redis room state（HSET: `merchant_id`、`status="live"`、`current_item_id`、`online_count=0`）。
- `EndRoom` 成功后先更新 MySQL，清空 MySQL `current_item_id`，再软失败更新 Redis room state 的 `status="idle"` 和 `current_item_id=""`。
- `GetRoom` 和 `ListRooms` 从 Redis 读取 `online_count` 和 `item_queue`；Redis miss 或读取错误时降级返回 0 和空数组，不报错。
- `GetMerchantRoom` 从 Redis 读取 `online_count` 和 `queued_count`（队列长度）。
- `RoomDetailDTO` 和 `MerchantRoomDTO` 必须返回 `current_item_id` 字段；没有当前拍品时值为空字符串，不能省略字段。
- 当前拍品推进由商品模块维护：商品开始竞拍时写入 MySQL `live_rooms.current_item_id` 和 Redis `auction:room:{room_id}:state.current_item_id`；商品取消、过期结束或房间下播时清空。
- `ListRooms` 不传 `status` 时默认只返回 `live` 状态的房间。
- `findMerchantRoom` 验证商家归属时若不属于当前商家，返回 `ErrNotFound`，不暴露他人房间是否存在。
- `item_queue` 在 `RoomDetailDTO` 中永远不为 nil，最少返回空数组。

根据当前代码结构推断：

- MySQL 是房间状态的唯一真相来源；Redis 状态滞后或缺失时，查询接口仍以 MySQL `LiveRoom.Status` 为准。
- `StartRoom` / `EndRoom` 的 Redis 写失败不会返回错误，也没有当前模块内可见告警。
- 公开房间列表当前没有分页，返回符合状态过滤的全量列表。

需确认内容集中在“需用户确认的问题”章节。

## 8. 业务不变量

- 每个商家只能有一个直播间，不能创建第二个。
- 直播间状态只能在 `idle` 和 `live` 之间流转，不能跳过或进入未定义状态。
- `live` 状态下不能再次开播。
- `idle` 状态下不能执行下播。
- MySQL 是房间状态的唯一真相来源；Redis 状态滞后或缺失时，接口响应仍应基于 MySQL 状态。
- 非商家身份无法创建或管理直播间。
- 非所属商家无法开播或下播其他商家的直播间。
- 公开详情的 `item_queue` 必须是数组，不能为 `null`。
- 公开列表、公开详情和商家视图必须包含 `current_item_id` 字段；没有当前拍品时返回空字符串，不能因空值省略。
- `current_item_id` 不能在取消拍品、过期结束或下播后保留旧商品 ID。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 9. 字段规则索引

### CreateRoomRequest / LiveRoom

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `title` | request/db/response | 必填；HTTP 长度 1 到 128；Service 创建时 trim；幂等激活不更新已有 title | `POST /api/v1/merchant/room` | `ROOM.FIELD.title.*` |
| `id` | db/response/path | 房间 ID 使用 `room_` 前缀；主键长度 64 | 全部房间接口 | `ROOM.FIELD.id.*` |
| `merchant_id` | auth/db/response | 创建时来自当前商家；唯一索引；状态动作必须匹配当前商家 | 商家房间接口、状态动作 | `ROOM.FIELD.merchant_id.*` |
| `status` | db/response/Redis | 枚举仅 `idle`、`live`；状态动作只能 `idle <-> live` | 列表、详情、开播、下播 | `ROOM.FIELD.status.*` |
| `current_item_id` | db/response/Redis | 可空；公开列表、公开详情和商家视图必须返回该字段，空值为 `""`；商品开始竞拍时写入，商品取消、过期结束或房间下播时清空 | 列表、详情、商家视图、Redis state、商品开始/取消/过期结束、下播 | `ROOM.FIELD.current_item_id.*` |
| `created_at` / `updated_at` | db/response | GORM 自动维护；列表按 `created_at DESC` | 列表、详情、商家视图 | `ROOM.FIELD.timestamps.*` |
| `DeletedAt` | db | 模型支持软删除；当前无删除接口 | DAO 集成 | `ROOM.FIELD.deleted_at.*` |

### RoomDetailDTO / MerchantRoomDTO / Redis

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `online_count` | Redis/response | Redis room state 命中时使用 `online_count`；miss 或错误时为 0 | 公开详情、公开列表、商家视图 | `ROOM.FIELD.online_count.*` |
| `item_queue` | Redis/response | 来自 Redis ZSET `auction:room:{room_id}:item_queue` 的升序成员；nil 转为空数组 | `GET /api/v1/rooms/{room_id}`、`GET /api/v1/rooms` | `ROOM.FIELD.item_queue.*` |
| `queued_count` | Redis/response | 等于 item queue 长度；Redis miss 时为 0 | `GET /api/v1/merchant/room`、`POST /api/v1/merchant/room` 幂等返回 | `ROOM.FIELD.queued_count.*` |
| `status_text` | response | `idle -> 未开播`，`live -> 直播中`，未知状态返回原字符串 | 商家视图 | `ROOM.FIELD.status_text.*` |
| `actions.can_start` | response | `status == idle` 时为 true | 商家视图 | `ROOM.FIELD.actions.can_start.*` |
| `actions.can_end` | response | `status == live` 时为 true | 商家视图 | `ROOM.FIELD.actions.can_end.*` |
| Redis room state | Redis | key 为 `auction:room:{room_id}:state`，字段含 `merchant_id`、`status`、`current_item_id`、`online_count` | 开播、下播、查询 | `ROOM.FIELD.redis_state.*` |

## 10. 接口测试契约

### `POST /api/v1/merchant/room` 商家激活/获取直播间

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/room/router/room.go` | 鉴权组内 JSON 绑定 |
| Handler | `internal/app/room/handler/room.go` | `ActivateRoom`、`web.BindingErrors` |
| DTO | `internal/app/room/dto/room.go` | `CreateRoomRequest`、`MerchantRoomDTO` |
| Service | `internal/app/room/service/service.go` | `ActivateRoom` |
| DAO | `internal/app/room/dao/room.go` | `FindRoomByMerchantID`、`CreateRoom` |
| Cache | `internal/app/room/cache/cache.go` | 幂等返回时读取 room state 和 item queue |

#### 接口职责

商家首次调用时创建直播间；已有直播间时返回现有直播间。该接口不负责开播、下播、删除或修改已有 title。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `title` | 是 | 长度 1 到 128；首次创建时 trim；已有房间时不更新 | 参数错误或返回已有房间 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data.id` | `room_` 前缀；重复激活返回同一 ID | HTTP 响应 + DB |
| `data.status` | 首次创建为 `idle`；重复激活返回当前状态 | HTTP 响应 + DB |
| `data.status_text` | 与状态对应 | HTTP 响应 |
| `data.queued_count` | 已有房间时来自 Redis item queue 长度；新建为 0 | HTTP 响应 + Redis |
| `data.actions` | `idle` 可 start，`live` 可 end | HTTP 响应 |

#### 测试数据准备

- 商家 token、普通用户 token、无效 token、未登录请求。
- 合法 title、空 title、超长 title。
- 幂等场景需准备商家已有房间和 Redis item queue。

#### 成功路径

- 商家首次激活成功，创建 `idle` 状态房间，ID 含 `room_` 前缀。
- 商家重复激活返回同一房间 ID，数据库只有 1 条该商家的直播间记录，title 不被覆盖。
- 幂等返回时 `queued_count` 来自 Redis item queue 长度。

#### 失败路径

- 未登录、普通用户或无效 token 返回未授权。
- 缺少 `title`、title 为空或超过 128 字符返回参数错误。
- DAO 创建失败返回错误响应，不能留下半成功状态。

#### 状态和一致性验证

- HTTP 返回的 ID、商家 ID、状态、title 与 `live_rooms` 记录一致。
- 重复激活后 `merchant_id` 唯一性成立。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证权限、幂等、ID 前缀和 queued_count |
| 接口契约测试 | 是 | 验证绑定、鉴权和响应结构 |
| 模块集成测试 | 是 | 验证唯一索引和 DB 记录 |
| 场景测试 | 是 | 激活并开播、激活幂等场景覆盖 |
| 并发测试 | 是 | 同一商家并发激活 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、Redis |

### `GET /api/v1/merchant/room` 商家查看自己直播间

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/room/router/room.go` | 鉴权组内路由 |
| Handler | `internal/app/room/handler/room.go` | `GetMerchantRoom` |
| DTO | `internal/app/room/dto/room.go` | `MerchantRoomDTO` |
| Service | `internal/app/room/service/service.go` | `GetMerchantRoom` |
| DAO | `internal/app/room/dao/room.go` | `FindRoomByMerchantID` |
| Cache | `internal/app/room/cache/cache.go` | 读取 room state 和 item queue |

#### 接口职责

返回当前商家的直播间状态、在线人数、待拍数量和可操作动作。

#### 请求字段

无请求体；身份来自 token。

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `id` / `merchant_id` / `title` / `status` | 与 DB 一致 | HTTP 响应 + DB |
| `current_item_id` | 必须出现；与 DB `live_rooms.current_item_id` 一致；无当前拍品时为空字符串 | HTTP 响应 + DB |
| `online_count` | Redis 命中时使用 Redis；miss 时为 0 | HTTP 响应 + Redis |
| `queued_count` | Redis item queue 长度；miss 时为 0 | HTTP 响应 + Redis |
| `actions` | 与状态一致 | HTTP 响应 |

#### 测试数据准备

- 商家有房间和无房间两种状态。
- Redis state 和 item queue 命中/miss 场景。

#### 成功路径

- 商家有房间时返回完整 `MerchantRoomDTO`。
- idle 状态 `actions.can_start=true`、`actions.can_end=false`。
- live 状态 `actions.can_start=false`、`actions.can_end=true`。

#### 失败路径

- 未登录、普通用户或无效 token 返回未授权。
- 商家无房间返回 not found。
- Redis miss 或错误不应导致接口失败。

#### 状态和一致性验证

- HTTP 与 DB 状态一致。
- `online_count` 和 `queued_count` 与 Redis 一致或按降级规则为 0。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证身份、not found、派生字段 |
| 接口契约测试 | 是 | 验证响应结构和错误 |
| 模块集成测试 | 是 | 验证 DB 查询和 Redis 富化 |
| 场景测试 | 是 | 开播、下播、幂等场景覆盖 |
| 并发测试 | 否 | 读取接口不作为并发目标 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、Redis |

### `POST /api/v1/rooms/{room_id}/start` 商家开播

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/room/router/room.go` | 鉴权组内状态动作 |
| Handler | `internal/app/room/handler/room.go` | `StartRoom` |
| Service | `internal/app/room/service/service.go` | `StartRoom`、`findMerchantRoom` |
| DAO | `internal/app/room/dao/room.go` | `FindRoomByID`、`UpdateRoom` |
| Cache | `internal/app/room/cache/cache.go` | `InitRoomState` |

#### 接口职责

将所属商家的 `idle` 房间切换为 `live`，并软失败初始化 Redis room state。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `room_id` | 是 | Service trim；必须存在、属于当前商家、状态为 `idle` | not found 或业务错误 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data` | 成功时为 `null` | HTTP 响应 |

#### 测试数据准备

- 当前商家 `idle` 房间。
- 当前商家 `live` 房间。
- 其他商家房间。
- Redis 可写和 Redis 写失败场景。

#### 成功路径

- 开播接口成功。
- DB 状态变为 `live`。
- Redis `auction:room:{room_id}:state` 包含 `status=live`、`merchant_id`、`online_count=0`。
- 后续公开详情和商家视图状态为 `live`。

#### 失败路径

- 未登录、普通用户或无效 token 返回未授权。
- 非所属商家操作返回 not found。
- 不存在 room_id 返回 not found。
- 对 `live` 房间重复开播返回业务错误。
- Redis 初始化失败当前软失败，HTTP 仍成功；需记录该行为。

#### 状态和一致性验证

- HTTP 成功、DB 状态、公开详情、商家视图一致。
- Redis 写失败时，报告需区分 MySQL 成功和 Redis 缺失。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证状态流转、归属和软失败 |
| 接口契约测试 | 是 | 验证状态动作响应 |
| 模块集成测试 | 是 | 验证 DB 和 Redis |
| 场景测试 | 是 | 激活并开播场景覆盖 |
| 并发测试 | 是 | 同一房间并发开播 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、Redis |

### `POST /api/v1/rooms/{room_id}/end` 商家下播

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/room/router/room.go` | 鉴权组内状态动作 |
| Handler | `internal/app/room/handler/room.go` | `EndRoom` |
| Service | `internal/app/room/service/service.go` | `EndRoom`、`findMerchantRoom` |
| DAO | `internal/app/room/dao/room.go` | `FindRoomByID`、`UpdateRoom` |
| Cache | `internal/app/room/cache/cache.go` | `UpdateRoomStatus` |

#### 接口职责

将所属商家的 `live` 房间切换为 `idle`，并软失败更新 Redis room state。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `room_id` | 是 | Service trim；必须存在、属于当前商家、状态为 `live` | not found 或业务错误 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data` | 成功时为 `null` | HTTP 响应 |

#### 测试数据准备

- 当前商家 `live` 房间。
- 当前商家 `idle` 房间。
- 其他商家房间。
- Redis state 存在和不存在两种情况。

#### 成功路径

- 下播接口成功。
- DB 状态变为 `idle`。
- Redis room state 的 `status` 更新为 `idle`。
- DB `current_item_id` 被清空；Redis room state 存在时 `current_item_id=""`。
- 后续商家视图 `can_start=true`、`can_end=false`。

#### 失败路径

- 未登录、普通用户或无效 token 返回未授权。
- 非所属商家操作返回 not found。
- 不存在 room_id 返回 not found。
- 对 `idle` 房间下播返回业务错误。
- Redis 更新失败当前软失败，HTTP 仍成功；需记录该行为。

#### 状态和一致性验证

- HTTP 成功、DB 状态、公开详情、商家视图一致。
- 下播后公开详情、公开列表和商家视图中的 `current_item_id` 均为空字符串。
- Redis 缺失或滞后时，MySQL 状态仍为接口状态来源。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证状态流转、归属和软失败 |
| 接口契约测试 | 是 | 验证状态动作响应 |
| 模块集成测试 | 是 | 验证 DB 和 Redis |
| 场景测试 | 是 | 下播场景覆盖 |
| 并发测试 | 是 | 同一房间并发下播 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、Redis |

### `GET /api/v1/rooms` 公开房间列表

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/room/router/room.go` | 公开路由，无鉴权 |
| Handler | `internal/app/room/handler/room.go` | `ListRooms` |
| DTO | `internal/app/room/dto/room.go` | `RoomDetailDTO` |
| Service | `internal/app/room/service/service.go` | `ListRooms`，默认 live 过滤 |
| DAO | `internal/app/room/dao/room.go` | `ListRooms`，按 `created_at DESC` |
| Cache | `internal/app/room/cache/cache.go` | 读取 room state 和 item queue |

#### 接口职责

返回公开房间列表；不传 `status` 时默认只返回直播中房间。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `status` | 否 | 空值默认 `live`；当前未知值按字符串过滤 | 需确认是否应参数错误 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data` | 数组；元素为 `RoomDetailDTO` | HTTP 响应 |
| `status` | 不传参数时均为 `live` | HTTP 响应 + DB |
| `current_item_id` | 每个房间元素必须出现；与 DB `live_rooms.current_item_id` 一致；无当前拍品时为空字符串 | HTTP 响应 + DB |
| `online_count` | Redis 命中时使用 Redis，否则 0 | HTTP 响应 + Redis |
| `item_queue` | 数组，不能为 null | HTTP 响应 |

#### 测试数据准备

- 至少一个 `live` 房间和一个 `idle` 房间。
- Redis state 和 item queue 命中/miss 场景。

#### 成功路径

- 不传 `status` 时只返回 `live` 房间。
- `status=idle` 时只返回 `idle` 房间。
- 每个返回元素都包含 `current_item_id`；无当前拍品时为空字符串。
- Redis 命中时富化在线人数和待拍队列。
- Redis miss 时返回 0 和空数组。

#### 失败路径

- DAO 查询失败返回错误。
- Redis 读取失败不应导致接口失败。
- 未知 `status` 当前返回数据库过滤结果，需记录行为。

#### 状态和一致性验证

- HTTP 列表与 DB 状态过滤一致。
- Redis 富化字段与 Redis 状态一致或按降级规则返回。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证默认过滤和 Redis 降级 |
| 接口契约测试 | 是 | 验证公开访问和响应结构 |
| 模块集成测试 | 是 | 验证 DB 排序和过滤 |
| 场景测试 | 是 | 公开列表只展示直播中房间场景覆盖 |
| 并发测试 | 否 | 列表读取不作为并发目标 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、Redis |

### `GET /api/v1/rooms/{room_id}` 公开房间详情

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/room/router/room.go` | 公开详情路由 |
| Handler | `internal/app/room/handler/room.go` | `GetRoom` |
| DTO | `internal/app/room/dto/room.go` | `RoomDetailDTO` |
| Service | `internal/app/room/service/service.go` | `GetRoom` |
| DAO | `internal/app/room/dao/room.go` | `FindRoomByID` |
| Cache | `internal/app/room/cache/cache.go` | 读取 room state 和 item queue |

#### 接口职责

返回公开房间详情、在线人数和待拍商品队列；不要求登录。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `room_id` | 是 | Service trim；不存在返回 not found | not found |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `id` / `merchant_id` / `title` / `status` | 与 DB 一致 | HTTP 响应 + DB |
| `current_item_id` | 必须出现；与 DB `live_rooms.current_item_id` 一致；无当前拍品时为空字符串 | HTTP 响应 + DB |
| `online_count` | Redis 命中时使用 Redis；miss 时为 0 | HTTP 响应 + Redis |
| `item_queue` | Redis ZSET 升序成员；无数据时为空数组 | HTTP 响应 + Redis |

#### 测试数据准备

- 存在的房间、不存在的 room_id。
- Redis state 命中/miss。
- 通过商品模块上架得到的 item queue。

#### 成功路径

- 房间存在时返回详情。
- 响应始终包含 `current_item_id` 字段；没有当前拍品时为 `""`。
- Redis `online_count` 富化到响应。
- `item_queue` 包含 Redis ZSET 成员，顺序为 score 升序。
- Redis miss 时 `online_count=0`、`item_queue=[]`，接口成功。

#### 失败路径

- 不存在房间返回 not found。
- DAO 错误返回错误。
- Redis 读取错误不应导致接口失败。

#### 状态和一致性验证

- HTTP 基础字段与 DB 一致。
- `current_item_id` 与 DB 一致，且空值时字段不能被省略。
- `item_queue` 与 Redis ZSET 一致，且不能为 null。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证 trim、not found、Redis 降级 |
| 接口契约测试 | 是 | 验证公开访问和响应结构 |
| 模块集成测试 | 是 | 验证 DB 和 Redis 读取 |
| 场景测试 | 是 | 公开详情、商品队列富化场景覆盖 |
| 并发测试 | 否 | 详情读取不作为并发目标 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、Redis |

## 11. Service / DAO 测试契约

### `ActivateRoom` / `CreateRoom`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/room/service/service.go` | 商家身份、幂等、ID 生成、title trim |
| Store 接口 | `internal/app/room/dao/room.go` | `FindRoomByMerchantID`、`CreateRoom` |
| DAO 实现 | `internal/app/room/dao/room.go` | GORM 创建、唯一索引 |
| Model | `internal/app/room/model/room.go` | `LiveRoom` 字段和索引 |

#### 测试数据准备

- fake store 初始为空或已有商家房间。
- fake cache 可返回 item queue。
- 真实数据库集成测试使用本批次 title 前缀。

#### 单元测试点

- 普通用户激活直播间返回未授权。
- 商家首次激活成功，创建 `idle` 状态房间，ID 含 `room_` 前缀。
- 商家再次激活返回同一房间 ID，store 只有 1 条记录。
- 激活已有直播间时返回的 `queued_count` 来自 Redis item_queue 长度。
- title 包含首尾空格时，创建后的 title 已去除空格。

#### 集成测试点

- 同一商家唯一索引阻止第二个直播间。
- 并发激活后该商家只有 1 条房间记录。

### `StartRoom` / `EndRoom`

#### 测试数据准备

- fake store 中准备 `idle`、`live`、其他商家房间。
- fake cache 可模拟写成功和写失败。
- 真实 Redis 使用本批次 room ID。

#### 单元测试点

- `StartRoom` 将 `idle` 房间切换到 `live`，store 状态为 `live`。
- `StartRoom` 对 `live` 状态房间返回 `ErrInvalidRequest`。
- `StartRoom` 成功后 Redis state 写入 `status="live"`。
- `EndRoom` 将 `live` 房间切换到 `idle`，store 状态为 `idle`。
- `EndRoom` 对 `idle` 状态房间返回 `ErrInvalidRequest`。
- `EndRoom` 成功后 Redis state 更新 `status="idle"`。
- `EndRoom` 成功后 store 中 `current_item_id` 清空，Redis state 存在时 `current_item_id` 清空。
- 非商家调用 `StartRoom`/`EndRoom` 返回未授权。
- 非所属商家调用 `StartRoom`/`EndRoom` 返回 `ErrNotFound`。
- Redis 写失败不影响 Service 返回成功。

#### 集成测试点

- DB 状态与真实 Redis state 一致或按软失败规则记录。
- 并发状态动作最终只能处于合法状态。

### `GetRoom` / `ListRooms` / `GetMerchantRoom`

#### 测试数据准备

- fake store 中准备 idle/live 房间。
- fake cache 准备 state、queue、miss 和错误。

#### 单元测试点

- `GetRoom` 从 Redis 读取 `online_count` 富化到响应。
- `GetRoom` 从 Redis 读取 `item_queue` 富化到响应。
- `GetRoom` Redis miss 时降级返回 `online_count=0`、`item_queue=[]`，不报错。
- `GetRoom` / `ListRooms` / `GetMerchantRoom` 序列化时必须包含 `current_item_id` 字段，空值返回 `""`。
- `ListRooms` 不传 status 时默认返回 `live` 状态房间。
- `ListRooms` 传入 `status=idle` 只返回 `idle` 状态房间。
- `GetMerchantRoom` 非商家返回未授权。
- `GetMerchantRoom` 商家无房间时返回 not found。

#### 集成测试点

- 真实 DB 列表按 `created_at DESC`。
- 真实 Redis ZSET 的顺序和响应 `item_queue` 一致。

## 12. 核心场景测试

### 场景 1：商家激活并开播直播间

#### 业务价值

验证商家从无直播间到直播中的核心链路。

#### 关联接口 / 方法

- `POST /api/v1/merchant/room`
- `POST /api/v1/rooms/{room_id}/start`
- `GET /api/v1/rooms/{room_id}`
- `GET /api/v1/merchant/room`

#### 代码定位

Router、Handler、DTO、Service、DAO、Model、Cache 见第 2 节。

#### 测试数据准备

- 商家账号和有效 token。
- 合法 title。
- 清理本批次房间和 Redis room state。

#### Given

- 商家账号已创建，持有有效 token。

#### When

- 调用 `POST /api/v1/merchant/room` 激活直播间。
- 使用返回的 `id` 调用 `POST /api/v1/rooms/{room_id}/start`。
- 调用 `GET /api/v1/rooms/{room_id}` 查询公开详情。
- 调用 `GET /api/v1/merchant/room` 查询商家视图。

#### Then

- 激活接口返回 `id`（含 `room_` 前缀）、`status=idle`、`actions.can_start=true`。
- 开播接口返回成功。
- 公开详情 `status=live`，`online_count` 为数字。
- 商家视图 `status=live`、`status_text="直播中"`、`actions.can_start=false`、`actions.can_end=true`。
- 数据库 `live_rooms` 中该记录 `status=live`。
- Redis `auction:room:{room_id}:state` 存在时，`status` 字段为 `live`；若 Redis 写失败，报告记录软失败证据。

#### 证据要求

- HTTP 响应、数据库记录、Redis HGETALL 或软失败记录、清理结果。

### 场景 2：商家下播直播间

#### 业务价值

验证 live 房间能回到 idle，并更新商家操作按钮。

#### 关联接口 / 方法

- `POST /api/v1/rooms/{room_id}/end`
- `GET /api/v1/merchant/room`
- `EndRoom`

#### Given

- 商家直播间处于 `live` 状态。

#### When

- 调用 `POST /api/v1/rooms/{room_id}/end`。
- 调用 `GET /api/v1/merchant/room`。

#### Then

- 下播接口返回成功。
- 商家视图 `status=idle`、`status_text="未开播"`、`actions.can_start=true`、`actions.can_end=false`。
- 数据库状态为 `idle`。
- Redis `auction:room:{room_id}:state` 存在时 `status=idle`；若 Redis 缺失，记录降级行为。

#### 证据要求

- HTTP 响应、数据库记录、Redis state 或降级证据。

### 场景 3：激活直播间幂等

#### 业务价值

验证每个商家只有一个直播间。

#### 关联接口 / 方法

- `POST /api/v1/merchant/room`
- `ActivateRoom`

#### Given

- 商家已有 `idle` 状态直播间（id=room_A）。

#### When

- 再次调用 `POST /api/v1/merchant/room`（title 不同）。

#### Then

- 返回 `id=room_A`。
- 数据库只有 1 条该商家的直播间记录。
- title 未被覆盖，返回原 title。

#### 证据要求

- 两次 HTTP 响应、数据库计数、数据库 title。

### 场景 4：直播间归属隔离

#### 业务价值

验证商家不能操作其他商家的直播间，且不暴露资源存在性。

#### 关联接口 / 方法

- `POST /api/v1/rooms/{room_id}/start`
- `POST /api/v1/rooms/{room_id}/end`
- `findMerchantRoom`

#### Given

- 商家 A 和商家 B 各自激活了直播间。

#### When

- 商家 B 调用 `POST /api/v1/rooms/{merchant_A_room_id}/start`。
- 商家 B 调用 `POST /api/v1/rooms/{merchant_A_room_id}/end`。

#### Then

- 两次请求均返回 not found。
- 商家 A 的直播间状态没有发生变化。

#### 证据要求

- HTTP 响应、商家 A 房间前后 DB 状态。

### 场景 5：公开列表只展示直播中房间

#### 业务价值

验证用户默认只看到 live 房间。

#### 关联接口 / 方法

- `GET /api/v1/rooms`
- `ListRooms`

#### Given

- 商家 A 的直播间为 `live`，商家 B 的直播间为 `idle`。

#### When

- 不带参数调用 `GET /api/v1/rooms`。

#### Then

- 响应列表只包含 `live` 状态房间。
- `idle` 房间不出现在列表中。

#### 证据要求

- HTTP 响应、数据库状态。

### 场景 6：GetRoom 含商品队列富化

#### 业务价值

验证房间详情能展示商品模块写入的待拍队列。

#### 关联接口 / 方法

- 商品模块 `POST /api/v1/items/{item_id}/publish`
- `GET /api/v1/rooms/{room_id}`
- Redis `auction:room:{room_id}:item_queue`

#### Given

- 直播间已存在。
- 通过商品模块上架 2 个本批次商品，使 Redis `item_queue` ZSET 中有 2 个 item_id。

#### When

- 调用 `GET /api/v1/rooms/{room_id}`。

#### Then

- 响应 `item_queue` 包含这 2 个 item_id，顺序为 ZSET 升序（score 从低到高）。
- `item_queue` 为空时也必须返回 `[]`，不能为 `null`。

#### 证据要求

- 商品上架 HTTP 响应、Redis ZSET、房间详情 HTTP 响应。

### 场景 7：房间响应暴露并维护当前拍品 ID

#### 业务价值

验证用户和商家能从房间列表、房间详情和商家视图稳定拿到 `current_item_id`，避免当前拍品为空时字段消失，或拍品结束后残留旧 ID。

#### 关联接口 / 方法

- `GET /api/v1/rooms`
- `GET /api/v1/rooms/{room_id}`
- `GET /api/v1/merchant/room`
- 商品模块 `POST /api/v1/items/{item_id}/start`
- 商品模块 `POST /api/v1/items/{item_id}/cancel`
- `EndRoom`
- Redis `auction:room:{room_id}:state.current_item_id`

#### Given

- 商家直播间已激活并开播。
- 本批次商品已绑定该 `room_id` 并上架。

#### When

- 在未开始任何商品竞拍时，查询 `GET /api/v1/rooms`、`GET /api/v1/rooms/{room_id}` 和 `GET /api/v1/merchant/room`。
- 调用商品模块 `POST /api/v1/items/{item_id}/start` 开始竞拍。
- 再次查询房间列表、房间详情和商家视图。
- 调用商品模块 `POST /api/v1/items/{item_id}/cancel` 或调用 `POST /api/v1/rooms/{room_id}/end`。
- 第三次查询房间列表、房间详情和商家视图。

#### Then

- 未开始商品竞拍时，三个房间响应都包含 `current_item_id` 字段，值为 `""`。
- 商品开始竞拍后，三个房间响应中的 `current_item_id` 均等于该商品 ID。
- MySQL `live_rooms.current_item_id` 等于该商品 ID。
- Redis room state 存在时，`current_item_id` 等于该商品 ID。
- 商品取消或房间下播后，三个房间响应中的 `current_item_id` 均回到 `""`。
- MySQL `live_rooms.current_item_id` 被清空；Redis room state 存在时 `current_item_id=""`。

#### 证据要求

- 三类 HTTP 响应、MySQL `live_rooms.current_item_id`、Redis `HGET auction:room:{room_id}:state current_item_id`、商品开始/取消或房间下播响应、清理结果。

## 13. 状态流转和一致性测试

| 当前状态 | 动作 | 目标状态 | 允许 | 涉及接口 / 方法 | 一致性证据 |
| --- | --- | --- | --- | --- | --- |
| 无房间 | activate | `idle` | 是 | `POST /api/v1/merchant/room` / `ActivateRoom` | HTTP + DB |
| 已有房间 | activate | 不变 | 是 | `POST /api/v1/merchant/room` / `ActivateRoom` | HTTP + DB count |
| `idle` | start | `live` | 是 | `POST /api/v1/rooms/{room_id}/start` / `StartRoom` | HTTP + DB + Redis |
| `idle` | end | 不变 | 否 | `POST /api/v1/rooms/{room_id}/end` / `EndRoom` | 错误响应 + DB 不变 |
| `live` | start | 不变 | 否 | `POST /api/v1/rooms/{room_id}/start` / `StartRoom` | 错误响应 + DB 不变 |
| `live` | end | `idle` | 是 | `POST /api/v1/rooms/{room_id}/end` / `EndRoom` | HTTP + DB + Redis |
| `live` + 有当前拍品 | end | `idle` + `current_item_id=""` | 是 | `POST /api/v1/rooms/{room_id}/end` / `EndRoom` | HTTP + DB `current_item_id` + Redis `current_item_id` |
| `live` + 已上架商品 | start item | `live` + `current_item_id=item_id` | 是 | 商品模块 `POST /api/v1/items/{item_id}/start` | 房间 HTTP + DB `current_item_id` + Redis `current_item_id` |
| `live` + 当前拍品 | cancel/end item | `live` + `current_item_id=""` | 是 | 商品取消 / 过期结束 | 房间 HTTP + DB `current_item_id` + Redis `current_item_id` |
| 其他商家房间 | start/end | 不变 | 否 | 状态动作接口 / `findMerchantRoom` | not found + DB 不变 |
| 不存在房间 | start/end/get | 不变 | 否 | 对应接口 | not found |

状态不一致时，agent 必须记录哪两个数据源不一致、触发步骤，以及是否影响商家开播、下播或用户观看。

## 14. 并发测试

| 并发目标 | 是否需要 | 真实依赖 | 通过标准 |
| --- | --- | --- | --- |
| 同一商家并发 `ActivateRoom` 多次 | 是 | 测试数据库 / HTTP | `live_rooms` 表中该商家只有 1 条记录，错误可解释 |
| 同一直播间并发 `StartRoom` | 是 | 测试数据库 / Redis / HTTP | 最终状态为 `live`，不能进入未定义状态，DB 和后续查询一致 |
| 同一直播间并发 `EndRoom` | 是 | 测试数据库 / Redis / HTTP | 最终状态为 `idle`，不能进入未定义状态，DB 和后续查询一致 |
| 商家 A 和商家 B 并发操作各自直播间 | 是 | 测试数据库 / Redis / HTTP | 互不影响 |

根据当前代码结构推断：

- 当前状态流转使用先查后保存，并发重复状态动作可能都返回成功或出现最后写入覆盖；如需严格单成功语义，需要用户确认预期。

## 15. WebSocket / Redis / 外部副作用测试

| 副作用 | 触发动作 | 验证方式 | 清理要求 |
| --- | --- | --- | --- |
| Redis `auction:room:{room_id}:state` 初始化 | `StartRoom` / `POST /start` | `HGETALL` 验证 `merchant_id`、`status=live`、`current_item_id=""`、`online_count=0` | 删除本批次 room state |
| Redis `auction:room:{room_id}:state` 状态更新 | `EndRoom` / `POST /end` | `HGETALL` 验证 `status=idle`、`current_item_id=""` | 删除本批次 room state |
| Redis `auction:room:{room_id}:state.current_item_id` 写入 | 商品模块 `StartItem` / `POST /items/{item_id}/start` | `HGET current_item_id` 与 MySQL `live_rooms.current_item_id`、房间 HTTP 响应一致 | 删除本批次 room state |
| Redis `auction:room:{room_id}:state.current_item_id` 清空 | 商品取消、商品过期结束、`EndRoom` | `HGET current_item_id` 为空字符串；MySQL 和房间 HTTP 响应同步为空 | 删除本批次 room state |
| Redis `auction:room:{room_id}:item_queue` 读取 | `GetRoom` / `ListRooms` / `GetMerchantRoom` | 通过商品模块上架后 `ZRANGE` 与 HTTP 响应对比 | 清理本批次 item queue 或移除本批次 item member |
| WebSocket 消息 | 不适用 | 当前房间模块没有开播/下播广播 | 如需验证推送，转到实时广播模块或跨模块流程 |
| 第三方外部服务 | 不适用 | 房间模块当前不应调用真实第三方 | 不允许引入真实第三方依赖 |

## 16. 回归测试

| 风险 | 回归测试位置 | 触发条件 | 证据 |
| --- | --- | --- | --- |
| 同一商家激活两次创建两个房间 | 单元 / 集成 / 并发 | 激活逻辑或唯一索引变更 | DB count + HTTP 响应 |
| 普通用户或未登录用户可以激活、开播或下播 | 单元 / 接口 | 鉴权变更 | 错误响应或单元断言 |
| 非所属商家可以操作其他商家的房间 | 单元 / 接口 / 场景 | 归属校验变更 | not found + DB 不变 |
| 开播成功但数据库状态仍为 `idle` | 单元 / 集成 | 状态保存变更 | DB + HTTP 详情 |
| 下播成功但数据库状态仍为 `live` | 单元 / 集成 | 状态保存变更 | DB + HTTP 详情 |
| `live` 房间重复开播成功 | 单元 / 接口 | 状态规则变更 | 错误响应 |
| `idle` 房间重复下播成功 | 单元 / 接口 | 状态规则变更 | 错误响应 |
| Redis miss 时 `GetRoom` 报错 | 单元 / 接口 | 降级逻辑变更 | HTTP 成功响应 |
| `item_queue` 为 `null` | 单元 / 接口 | DTO 构造变更 | HTTP 响应 |
| `current_item_id` 空值时被省略 | 单元 / 接口 | DTO json tag 或响应 DTO 变更 | HTTP 响应包含 `current_item_id:""` |
| 商品开始竞拍后房间响应没有当前拍品 ID | 单元 / 场景 / 状态一致性 | 商品开始逻辑、room current item 写入或查询响应变更 | 房间 HTTP + DB + Redis |
| 商品取消、过期结束或下播后 `current_item_id` 残留旧 ID | 单元 / 场景 / 状态一致性 | 清理逻辑变更 | 房间 HTTP + DB + Redis |
| `ListRooms` 不传 status 返回 `idle` 房间 | 单元 / 接口 | 默认过滤变更 | HTTP 响应 + DB |
| 公开接口要求登录 | 接口 | 路由鉴权变更 | 未登录 HTTP 响应 |
| 激活已有房间时 title 被覆盖 | 单元 / 接口 | 幂等行为变更 | DB title |
| Redis 写失败导致开播/下播失败 | 单元 / 接口 | 软失败语义变更 | HTTP 响应 + fake cache |

## 17. 测试类型覆盖矩阵

| 测试对象 | 单元 | 接口契约 | 集成 | 场景 | 异常 | 边界 | 并发 | 状态一致性 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `POST /api/v1/merchant/room` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `GET /api/v1/merchant/room` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `POST /api/v1/rooms/{room_id}/start` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `POST /api/v1/rooms/{room_id}/end` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `GET /api/v1/rooms` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `GET /api/v1/rooms/{room_id}` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `current_item_id` 响应字段 | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `LiveRoom` 字段和唯一索引 | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| Redis room state / item queue | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| 房间归属隔离 | 是 | 是 | 是 | 是 | 是 | 否 | 是 | 是 |
| WebSocket | 否 | 否 | 否 | 否 | 否 | 否 | 否 | 否 |

## 18. 通过标准

**核心验证点（全部通过才算过）：**

- 激活直播间接口响应结构符合统一响应格式，ID 含 `room_` 前缀，状态为 `idle`，有 HTTP 响应作为证据。
- 商家幂等激活不重复创建房间，有 store 断言或数据库查询作为证据。
- 商家权限和房间归属隔离成立，非所属商家操作返回 not found，有接口响应或单元测试作为证据。
- 状态流转只能 `idle ↔ live`，非法状态动作返回错误，有接口响应或单元测试作为证据。
- MySQL 状态与后续查询接口一致，有 HTTP 响应和数据库记录作为证据。
- `current_item_id` 在公开列表、公开详情和商家视图中始终出现；无当前拍品时为空字符串，有 HTTP 响应作为证据。
- 商品开始竞拍后 `current_item_id` 在房间 HTTP 响应、MySQL 和 Redis 中一致；商品取消、过期结束或下播后同步清空，有 HTTP、DB、Redis 证据。
- Redis miss 时 `GetRoom`、`ListRooms`、`GetMerchantRoom` 降级正常，有单元测试或接口测试作为证据。
- `item_queue` 在公开响应中为数组，且与 Redis ZSET 一致或按降级规则为空数组。

**辅助验证点（建议验证，可附说明跳过）：**

- Redis state 在 `StartRoom` 后写入正确字段，在 `EndRoom` 后更新 status 字段并清空 `current_item_id`。
- `status_text`、`queued_count`、`actions` 派生字段与 status 一致。
- 房间模块自动迁移成功创建或更新 `live_rooms` 表结构。
- `ListRooms` 过滤、排序和 `GetRoom` roomID trim 行为符合预期。
- 错误响应中的业务错误码与 `pkg/errorx` 定义一致。

## 19. 需用户确认的问题

- `ListRooms` 传入未知 status 值时，当前 DAO 直接按字符串过滤返回空列表；是否应返回参数错误？
- `ActivateRoom` 幂等时是否应更新 title 为新传入值？当前实现不更新。
- `StartRoom` Redis 初始化软失败时是否需要记录告警日志；当前代码静默丢弃错误。
- `EndRoom` Redis 状态和 `current_item_id` 清理软失败时是否需要记录告警日志；当前代码静默丢弃错误。
- 直播间删除场景（软删除）：当前实现无删除接口，是否需要？
- `item_queue` 排列顺序依赖 Redis ZSET score（由商品上架时的时间戳写入），是否需要文档明确顺序语义？
- 公开 `GET /api/v1/rooms` 接口是否需要分页？当前实现返回全量列表。
- 并发状态动作预期是严格只有一个请求成功，还是允许幂等成功？

## 20. 失败报告格式

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
