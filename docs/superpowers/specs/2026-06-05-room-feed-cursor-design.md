# Room Feed Cursor 接口设计

## 背景

现有公开直播间列表 `GET /api/v1/rooms` 由 room 模块提供，默认返回 `live` 状态直播间，并按 `created_at DESC` 全量查询。短视频式浏览不是传统分页场景，客户端会从当前条目继续向下预取下一批直播间。使用 page/page_size 时，如果直播间在滑动过程中新增、下播或排序位置变化，容易出现重复或漏项。

本设计新增一个专用 Feed 接口，只返回正在直播的房间，并使用基于稳定排序键的游标继续加载。

## 目标

- 为 C 端短视频式上下滑提供直播间 feed。
- 只返回 `live` 直播间。
- 支持客户端按批次预拉，避免 page/page_size 在动态列表中的重复和跳项。
- 保留现有 `GET /api/v1/rooms` 行为，降低兼容风险。
- 继续沿用 room 模块当前 Redis 富化状态软失败策略。

## 非目标

- 不支持 `idle`、回放或预告房间。
- 不做个性化推荐、权重排序或随机排序。
- 不新增全局 cursor/pagination 抽象。
- 不改变商家端直播间接口。

## 接口

新增公开接口：

```http
GET /api/v1/rooms/feed?cursor=&limit=10
```

请求参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `cursor` | string | 否 | 上一次响应返回的 `next_cursor`。为空时取第一页。 |
| `limit` | int | 否 | 每批返回数量。默认 10，最大 50。 |

响应数据：

```json
{
  "list": [
    {
      "id": "room_xxx",
      "merchant_id": "user_xxx",
      "title": "直播间",
      "status": "live",
      "current_item_id": "item_xxx",
      "online_count": 12,
      "item_queue": [],
      "item": [],
      "created_at": "2026-06-05T10:00:00+08:00",
      "updated_at": "2026-06-05T10:00:00+08:00"
    }
  ],
  "next_cursor": "base64-json",
  "has_more": true
}
```

当没有更多数据时：

- `has_more = false`
- `next_cursor = ""`

## 排序和游标

Feed 固定查询：

```sql
status = 'live'
ORDER BY created_at DESC, id DESC
```

游标包含上一批最后一个房间的：

- `created_at`
- `id`

建议编码为 URL-safe base64 JSON，例如：

```json
{"created_at":"2026-06-05T10:00:00.000000Z","id":"room_xxx"}
```

服务端解码后查询下一批：

```sql
status = 'live'
AND (
  created_at < :cursor_created_at
  OR (created_at = :cursor_created_at AND id < :cursor_id)
)
ORDER BY created_at DESC, id DESC
LIMIT :limit_plus_one
```

服务端实际查询 `limit + 1` 条，用多出的一条判断 `has_more`。返回给客户端的 `list` 最多 `limit` 条；`next_cursor` 使用返回列表最后一条生成。

使用 `(created_at, id)` 组合键是为了在多个房间创建时间相同的情况下保持确定性排序。

## 组件变更

### DTO

在 `internal/app/room/dto/room.go` 增加：

- `RoomFeedInput`
  - `Cursor string`
  - `Limit int`
- `RoomFeedResult`
  - `List []RoomDetailDTO`
  - `NextCursor string`
  - `HasMore bool`

`RoomDetailDTO` 继续复用现有结构。feed 列表中暂不填充完整 `Item` 列表，保持和现有 `ListRooms` 一致，只做 room state 和 item queue 富化。

### DAO

在 `internal/app/room/dao/room.go` 的 `Store` 增加：

```go
ListLiveRoomsByCursor(cursor *RoomFeedCursor, limit int) ([]*model.LiveRoom, error)
```

`RoomFeedCursor` 可以放在 dao 或 dto 中。它只表达持久层查询所需的 `CreatedAt` 和 `ID`，不包含编码逻辑。

GORM 查询固定 `status = live`，排序固定 `created_at DESC, id DESC`，limit 由 service 传入 `limit + 1`。

### Service

在 `internal/app/room/service/service.go` 增加：

```go
ListRoomFeed(ctx context.Context, input dto.RoomFeedInput) (*dto.RoomFeedResult, error)
```

处理流程：

1. 规范化 `limit`：默认 10，最大 50，小于等于 0 使用默认值。
2. 如果 `cursor` 不为空，解码并校验游标；非法游标返回 `errorx.ErrInvalidRequest`。
3. 调用 store 查询 `limit + 1` 条 live rooms。
4. 如果结果超过 `limit`，设置 `has_more = true` 并裁剪到 `limit`。
5. 对每个 room 按现有逻辑读取 Redis room state 和 item queue。Redis miss 或错误不阻断响应。
6. 使用裁剪后最后一条生成 `next_cursor`；没有更多数据时返回空字符串。

观测埋点使用 `room.feed`，记录安全字段：`limit`、`has_more`，不记录原始 cursor。

### Handler 和 Router

在 `internal/app/room/handler/room.go` 增加公开 handler：

```go
func ListRoomFeed(r flamego.Render, req *http.Request, c flamego.Context)
```

从 query 读取 `cursor` 和 `limit`，调用 service 后通过 `response.OK` 返回。

在 `internal/app/room/router/room.go` 增加路由：

```go
f.Get("/api/v1/rooms/feed", handler.ListRoomFeed)
```

该路由需要放在 `/api/v1/rooms/{room_id}` 之前，避免 `feed` 被识别为 `room_id`。

## 错误处理

| 场景 | 行为 |
| --- | --- |
| cursor 为空 | 返回第一页 |
| cursor 无法 base64 解码 | `ErrInvalidRequest` |
| cursor JSON 缺少 `created_at` 或 `id` | `ErrInvalidRequest` |
| cursor 时间格式非法 | `ErrInvalidRequest` |
| DAO 查询失败 | 返回原始错误，由 response 层处理 |
| Redis room state 读取失败 | 静默降级，`online_count = 0` |
| Redis item queue 读取失败 | 静默降级，`item_queue = []` |

## 测试

新增 room service 单元测试，继续使用 fake store/cache：

- 无 cursor 时只返回 `live` 房间。
- 第一批按 `created_at DESC, id DESC` 排序。
- 使用第一批 `next_cursor` 查询第二批，不重复返回第一批最后一条。
- 多个房间 `created_at` 相同时按 `id DESC` 稳定排序。
- `limit` 为空或小于等于 0 时使用默认 10。
- `limit` 超过上限时裁剪为 50。
- 非法 cursor 返回 `ErrInvalidRequest`。
- Redis state 和 item queue 读取失败时不阻断 feed 响应。

如需覆盖 handler，可增加轻量级 query 解析测试；核心行为以 service 和 DAO 单元测试为主。

## 兼容性

现有 `GET /api/v1/rooms` 保持不变。客户端短视频场景迁移到 `GET /api/v1/rooms/feed`，老客户端不会受影响。
