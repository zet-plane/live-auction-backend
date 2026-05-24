# 用户模块测试说明

## 1. 模块目标

用户模块负责账号密码注册、登录、JWT 鉴权、当前用户资料查询、资料修改和当前账号注销。

根据当前代码，用户模块同时为其他需要登录的模块提供 `AuthenticateToken` 鉴权能力，并用 `identity` 区分普通用户和商家。

## 2. 代码定位索引

| 对象 | 代码位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/user/router/user.go` | 注册用户模块 HTTP 路由和鉴权分组 |
| Handler | `internal/app/user/handler/user.go` | 处理请求绑定错误、调用 Service、返回统一响应 |
| DTO | `internal/app/user/dto/user.go` | 注册、登录、资料更新请求和用户响应 DTO |
| DTO 单元测试 | `internal/app/user/dto/user_test.go` | DTO 相关测试位置 |
| Service | `internal/app/user/service/service.go` | 注册、登录、鉴权、资料更新、注销业务逻辑 |
| Token Service | `internal/app/user/service/token.go` | JWT 签名、校验、过期判断 |
| Service 单元测试 | `internal/app/user/service/service_test.go` | 使用 `fakeStore` 测 Service 逻辑 |
| DAO | `internal/app/user/dao/user.go` | `dao.Store` 接口和 GORM 实现 |
| Model | `internal/app/user/model/user.go` | `User` GORM 模型、唯一索引、软删除字段 |
| 模块初始化 | `internal/app/user/init.go` | 模块依赖初始化 |
| Agent 测试契约 | `docs/agent-testing/modules/user.md` | 当前文档 |

当前用户模块未看到 Redis、WebSocket、外部第三方服务或后台任务依赖。

## 3. 测试边界

Agent 可以测试：

- 注册接口 `POST /api/v1/auth/register`。
- 登录接口 `POST /api/v1/auth/login`。
- 获取当前用户接口 `GET /api/v1/users/me`。
- 修改当前用户接口 `PUT /api/v1/users/me`。
- 注销当前用户接口 `DELETE /api/v1/users/me`。
- `Authorization: Bearer <token>` 鉴权中间件行为。
- `Register`、`Login`、`Authenticate`、`UpdateProfile`、`DeleteMe` Service 逻辑。
- `dao.Store` 接口契约和 `GormStore` 持久化行为。
- `User` 模型字段、账号唯一性、密码存储、软删除行为。
- JWT 签名、过期、篡改和用户不存在时的认证结果。
- HTTP 统一响应结构和错误码。

当前代码未看到用户模块使用 Redis、WebSocket 或外部第三方服务；这些依赖不属于用户模块测试边界。

## 4. 禁止事项

- 不测试支付、订单、物流、竞拍出价或竞拍场次状态流转。
- 不调用真实短信、邮件、支付或其他第三方服务。
- 不直接清空数据库。
- 不修改生产配置或复用线上真实用户账号。
- 不把测试账号用于本次测试以外的业务数据。
- 不在测试报告中写入真实 token、密码、数据库地址或可复用凭据。
- 不绕过业务接口直接修改用户状态，除非文档明确要求用于故障注入。
- 本地单元测试不允许直接连接数据库，必须使用 mock/fake store。
- Agent 连接线上或线上等价数据库时，只能操作本次测试创建的数据或带测试批次 ID 的数据。

## 5. 测试依赖策略

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | 使用 `service_test.go` 中的 `fakeStore` 或等价 mock Store、固定 token secret、固定时间；禁止直连数据库 | 稳定验证注册、登录、鉴权、资料更新和注销逻辑 |
| Agent 接口契约测试 | 使用真实 handler 或本地服务；允许连接真实测试数据库；不依赖 Redis 或 WebSocket | 验证真实请求绑定、响应结构、错误码和鉴权中间件 |
| Agent 模块集成测试 | 使用真实 `GormStore` 和测试数据库 | 验证账号唯一索引、软删除、字段持久化和自动迁移 |
| 场景测试 | 使用真实 HTTP 接口链路和真实测试数据库 | 验证注册、登录、查询、修改、注销这些用户可见链路 |
| Agent 并发测试 | 使用真实数据库唯一约束和真实 HTTP 并发请求 | mock 会掩盖重复注册、并发更新和注销/查询竞态 |
| 状态一致性测试 | 对比 HTTP 响应、后续查询接口和数据库记录 | 验证接口返回与持久化状态一致 |

## 6. 全局测试数据准备

需要准备：

- 测试批次 ID，例如 `agent_user_<YYYYMMDDHHMMSS>`。
- 至少 1 个普通用户注册请求，账号带测试批次前缀，例如 `agent_user_<batch>_alice`。
- 至少 1 个用于重复注册的相同账号。
- 至少 1 个错误密码登录请求。
- 至少 1 个有效 token。
- 至少 1 个格式错误 token。
- 至少 1 个签名被篡改 token。
- 至少 1 个过期 token。
- 至少 1 个用于资料更新的请求体，覆盖 `name`、`avatar_url`、`motto`、`identity`。
- 至少 1 组非法资料更新请求体，覆盖字段长度和非法 `identity`。

如果执行接口契约、模块集成、场景、并发或状态一致性测试，测试账号必须可识别，并在测试结束后验证清理或软删除结果。

## 7. 业务规则

- 注册请求包含 `account` 和 `password`。
- 注册成功后创建新用户，默认 `identity` 为 `user`。
- 注册成功后返回 token 和用户信息。
- `account` 在数据库中要求全局唯一。
- `password` 不允许出现在用户响应 DTO 中。
- 密码存储时必须是哈希结果，不能明文落库。
- 登录成功后返回 token 和用户信息。
- 登录账号不存在或密码错误时应返回未授权错误。
- 需要登录的接口必须携带 `Authorization: Bearer <token>`。
- token 签名错误、格式错误、过期或对应用户不存在时应认证失败。
- 当前用户资料允许修改 `name`、`avatar_url`、`motto`、`identity`。
- `identity` 只允许 `user` 或 `merchant`。
- 注销当前用户后，该用户不能继续通过旧 token 访问登录态接口。

根据当前代码结构推断：

- 注销通过 GORM `DeletedAt` 执行软删除。
- 用户可以通过修改资料接口把 `identity` 从 `user` 改为 `merchant`。

## 8. 业务不变量

- 同一个 `account` 不能存在两个有效用户。
- 响应中的用户信息不能包含 `password`。
- 数据库中保存的密码不能等于注册或登录时提交的明文密码。
- 未认证请求不能访问 `/api/v1/users/me` 下的接口。
- 无效 token 不能通过认证。
- 已注销用户不能继续被认证为当前用户。
- `identity` 不能被更新为 `user` 和 `merchant` 以外的值。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 9. 字段规则索引

### RegisterRequest / LoginRequest

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `account` | request/db/response | 注册接口绑定要求必填、长度 3 到 64；Service 层 trim；数据库唯一；登录时必填且 Service 层也要求长度 3 到 64 | `POST /api/v1/auth/register`、`POST /api/v1/auth/login`、`Register`、`Login` | `USER.FIELD.account.*` |
| `password` | request/db | 注册接口绑定要求必填、长度 6 到 72；Service 层 trim 后长度 6 到 72；数据库保存哈希；响应不得出现 | `POST /api/v1/auth/register`、`POST /api/v1/auth/login`、`Register`、`Login` | `USER.FIELD.password.*` |

### UpdateProfileRequest

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `name` | request/db/response | 可选；绑定层要求非空时长度 1 到 64；Service 层 trim | `PUT /api/v1/users/me`、`GET /api/v1/users/me`、`UpdateProfile` | `USER.FIELD.name.*` |
| `avatar_url` | request/db/response | 可选；绑定层最大 512；Service 层 trim | `PUT /api/v1/users/me`、`GET /api/v1/users/me`、`UpdateProfile` | `USER.FIELD.avatar_url.*` |
| `motto` | request/db/response | 可选；绑定层最大 255；Service 层 trim | `PUT /api/v1/users/me`、`GET /api/v1/users/me`、`UpdateProfile` | `USER.FIELD.motto.*` |
| `identity` | request/db/response | 可选；只允许 `user` 或 `merchant`；非法值返回身份错误或参数错误 | `PUT /api/v1/users/me`、`GET /api/v1/users/me`、`UpdateProfile` | `USER.FIELD.identity.*` |

### UserDTO / LoginResult

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `token` | response | 注册和登录成功时非空；可用于后续认证；不得写入报告 | `POST /api/v1/auth/register`、`POST /api/v1/auth/login` | `USER.FIELD.token.*` |
| `user.id` | db/response/token subject | 使用 `user_` 前缀；token subject 对应该 ID | 注册、登录、鉴权、当前用户查询 | `USER.FIELD.id.*` |
| `user.account` | db/response | 返回归一化后的账号 | 注册、登录、当前用户查询 | `USER.FIELD.user_account.*` |
| `user.password` | db only | 响应 DTO 不包含；数据库中必须是哈希 | 注册、登录、当前用户查询 | `USER.FIELD.user_password.*` |
| `created_at` / `updated_at` | db/response | DTO 中允许返回时间字段 | 注册、登录、当前用户查询 | `USER.FIELD.timestamps.*` |

## 10. 接口测试契约

### `POST /api/v1/auth/register` 注册

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/user/router/user.go` | `f.Post("/api/v1/auth/register", binding.JSON(dto.RegisterRequest{}), handler.Register)` |
| Handler | `internal/app/user/handler/user.go` | `Register` 处理绑定错误并调用 `svc.Register` |
| DTO | `internal/app/user/dto/user.go` | `RegisterRequest`、`RegisterInput`、`LoginResult`、`UserDTO` |
| Service | `internal/app/user/service/service.go` | `Register` 归一化账号、校验长度、查重、创建用户、签发 token |
| DAO | `internal/app/user/dao/user.go` | `FindUserByAccount`、`CreateUser` |
| Model | `internal/app/user/model/user.go` | `User.Account` 唯一索引、`Password` 响应隐藏 |

#### 接口职责

创建新用户账号，默认身份为 `user`，保存哈希密码，并返回 token 和用户信息。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `account` | 是 | 绑定层长度 3 到 64；Service 层 trim 后长度 3 到 64；数据库唯一 | 参数错误或业务错误 |
| `password` | 是 | 绑定层长度 6 到 72；Service 层 trim 后长度 6 到 72 | 参数错误或业务错误 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data.token` | 非空，可用于登录态接口 | HTTP 响应 |
| `data.user` | 包含 `id`、`account`、`name`、`identity`，不包含 `password` | HTTP 响应 |
| 数据库 `users.password` | 不等于明文密码 | 数据库记录 |

#### 测试数据准备

- 使用账号前缀 `agent_user_<batch>_`。
- 准备合法账号和密码。
- 准备重复账号请求。
- 准备账号为空、只有空格、长度 2、长度 65 的请求。
- 准备密码为空、只有空格、长度 5、长度 73 的请求。

#### 成功路径

- 合法账号密码注册成功。
- 返回非空 token。
- 返回用户 `identity=user`。
- 返回用户 `account` 是 trim 后账号。
- 数据库存在该用户。
- 数据库密码不是明文。

#### 失败路径

- 缺少 `account` 或 `password` 返回参数错误。
- `account` 低于 3 或超过 64 返回参数错误或业务错误。
- `password` 低于 6 或超过 72 返回参数错误或业务错误。
- 重复账号注册失败。

#### 状态和一致性验证

- 注册成功后 HTTP 返回的 `user.id` 与数据库记录一致。
- 注册失败不能创建用户记录。
- 重复注册不能出现两个有效同账号用户。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | 使用 fake Store 验证归一化、查重、密码哈希、默认身份和 token |
| 接口契约测试 | 是 | 使用真实测试数据库验证绑定、响应结构和错误码 |
| 模块集成测试 | 是 | 使用真实 GORM 验证唯一索引和持久化 |
| 场景测试 | 是 | 被“注册后登录并查询当前用户”覆盖 |
| 并发测试 | 是 | 并发重复注册同一账号只能有一个成功 |
| 状态一致性测试 | 是 | 对比 HTTP 响应和数据库记录 |

### `POST /api/v1/auth/login` 登录

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/user/router/user.go` | `f.Post("/api/v1/auth/login", binding.JSON(dto.LoginRequest{}), handler.Login)` |
| Handler | `internal/app/user/handler/user.go` | `Login` 处理绑定错误并调用 `svc.Login` |
| DTO | `internal/app/user/dto/user.go` | `LoginRequest`、`LoginResult` |
| Service | `internal/app/user/service/service.go` | `Login` 归一化账号、校验密码、签发 token |
| DAO | `internal/app/user/dao/user.go` | `FindUserByAccount` |
| Token | `internal/app/user/service/token.go` | `Sign` 生成 token |

#### 接口职责

用账号密码换取新的登录 token 和用户信息。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `account` | 是 | 绑定层只要求必填；Service 层 trim 后长度 3 到 64 | 参数错误或未授权 |
| `password` | 是 | 绑定层只要求必填；Service 层 trim 后长度 6 到 72 | 参数错误或未授权 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data.token` | 非空，可用于登录态接口 | HTTP 响应 |
| `data.user` | 不包含 `password` | HTTP 响应 |

#### 测试数据准备

- 已注册的测试用户。
- 正确密码。
- 错误密码。
- 不存在账号。
- 空账号、空密码请求。

#### 成功路径

- 正确账号密码登录成功。
- 返回 token 和用户信息。
- 返回用户信息与数据库记录一致。

#### 失败路径

- 登录账号不存在返回未授权。
- 密码错误返回未授权。
- 账号或密码为空返回参数错误或业务错误。
- 账号或密码 trim 后不满足 Service 长度规则返回参数错误。

#### 状态和一致性验证

- 登录不应修改用户基础资料。
- 登录返回用户不能包含密码。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | 使用 fake Store 验证成功、错误密码、不存在账号 |
| 接口契约测试 | 是 | 验证请求绑定、错误码和响应结构 |
| 模块集成测试 | 是 | 验证真实密码哈希比对和数据库查询 |
| 场景测试 | 是 | 被注册登录查询场景覆盖 |
| 并发测试 | 否 | 登录本身不改变关键状态 |
| 状态一致性测试 | 是 | 对比响应用户和数据库记录 |

### `GET /api/v1/users/me` 查询当前用户

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/user/router/user.go` | `/api/v1/users/me` 分组使用 `web.Authorization(handler.AuthenticateToken)` |
| Handler | `internal/app/user/handler/user.go` | `Me` 返回当前用户 DTO |
| DTO | `internal/app/user/dto/user.go` | `UserDTO` |
| Service | `internal/app/user/service/service.go` | `Authenticate` 验证 token 并查询用户 |
| Token | `internal/app/user/service/token.go` | `Verify` 验证 token 格式、签名、过期 |
| DAO | `internal/app/user/dao/user.go` | `FindUserByID` |

#### 接口职责

返回当前登录用户资料。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `Authorization` header | 是 | 必须是 `Bearer <token>`，token 签名有效、未过期、subject 对应有效用户 | 未授权 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data` | 当前用户 DTO，不包含 `password` | HTTP 响应 |

#### 测试数据准备

- 已注册并登录的测试用户。
- 有效 token。
- 缺失 header。
- 非 Bearer 格式 header。
- 格式错误 token。
- 签名篡改 token。
- 过期 token。
- 对应用户已注销或不存在的 token。

#### 成功路径

- 使用有效 token 查询成功。
- 响应用户 ID、账号、身份与数据库一致。
- 响应中不包含 `password`。

#### 失败路径

- 无 `Authorization` header 返回未授权。
- `Authorization` 不是 Bearer 格式返回未授权。
- token 分段数量错误、签名错误、payload 非法、过期、subject 为空返回未授权。
- token 对应用户不存在或已注销返回未授权。

#### 状态和一致性验证

- 查询不改变用户数据库记录。
- 注销后旧 token 再查询必须失败。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | 使用 fake Store 和固定时间验证 `Authenticate` |
| 接口契约测试 | 是 | 验证鉴权中间件和响应结构 |
| 模块集成测试 | 是 | 验证真实数据库中用户存在、软删除后的认证结果 |
| 场景测试 | 是 | 被注册登录查询、修改资料、注销失效场景覆盖 |
| 并发测试 | 可选 | 可与注销并发验证最终授权结果 |
| 状态一致性测试 | 是 | 对比响应和数据库记录 |

### `PUT /api/v1/users/me` 修改当前用户

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/user/router/user.go` | `f.Put("", binding.JSON(dto.UpdateProfileRequest{}), handler.UpdateMe)` |
| Handler | `internal/app/user/handler/user.go` | `UpdateMe` 处理绑定错误并调用 `svc.UpdateProfile` |
| DTO | `internal/app/user/dto/user.go` | `UpdateProfileRequest`、`UpdateProfileInput` |
| Service | `internal/app/user/service/service.go` | `UpdateProfile` trim 字段并校验 identity |
| DAO | `internal/app/user/dao/user.go` | `UpdateUser` |
| Model | `internal/app/user/model/user.go` | `Name`、`AvatarURL`、`Motto`、`Identity` 字段限制 |

#### 接口职责

修改当前登录用户资料，包括昵称、头像、签名和身份。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `name` | 否 | 非空时长度 1 到 64；Service 层 trim | 参数错误或业务错误 |
| `avatar_url` | 否 | 最大 512；Service 层 trim | 参数错误 |
| `motto` | 否 | 最大 255；Service 层 trim | 参数错误 |
| `identity` | 否 | 只允许 `user` 或 `merchant` | 参数错误或身份错误 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data` | 成功时为 `null` | HTTP 响应 |

#### 测试数据准备

- 已注册并登录的测试用户。
- 有效 token。
- 合法资料更新请求体。
- `name` 空字符串、`avatar_url` 超长、`motto` 超长、非法 `identity` 请求体。

#### 成功路径

- 修改 `name`、`avatar_url`、`motto` 成功。
- 修改 `identity=user` 或 `identity=merchant` 成功。
- 再次查询当前用户返回最新资料。

#### 失败路径

- 未登录修改返回未授权。
- 字段长度违反绑定规则返回参数错误。
- 非法 `identity` 返回参数错误或身份错误。

#### 状态和一致性验证

- HTTP 成功后数据库记录与后续查询一致。
- 失败时数据库中原资料不应被修改。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | 使用 fake Store 验证 trim、identity 校验和更新 |
| 接口契约测试 | 是 | 验证绑定、鉴权和响应结构 |
| 模块集成测试 | 是 | 验证真实数据库字段持久化 |
| 场景测试 | 是 | 被“修改资料并升级为商家身份”覆盖 |
| 并发测试 | 可选 | 并发更新同一用户时验证最终状态可解释 |
| 状态一致性测试 | 是 | 对比修改响应、查询响应和数据库记录 |

### `DELETE /api/v1/users/me` 注销当前用户

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/user/router/user.go` | `f.Delete("", handler.DeleteMe)` |
| Handler | `internal/app/user/handler/user.go` | `DeleteMe` 调用 `svc.DeleteMe` |
| Service | `internal/app/user/service/service.go` | `DeleteMe` 调用 Store 删除当前用户 |
| DAO | `internal/app/user/dao/user.go` | `DeleteUser` 使用 GORM 删除 |
| Model | `internal/app/user/model/user.go` | `DeletedAt` 软删除字段 |

#### 接口职责

注销当前登录用户，使旧 token 不能继续访问登录态接口。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `Authorization` header | 是 | 必须是有效登录 token | 未授权 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data` | 成功时为 `null` | HTTP 响应 |

#### 测试数据准备

- 已注册并登录的测试用户。
- 有效 token。
- 用于注销后复查的同一个 token。

#### 成功路径

- 使用有效 token 注销成功。
- 再次使用旧 token 查询当前用户返回未授权。
- 数据库中该用户为已删除状态或无法通过普通查询查到。

#### 失败路径

- 未登录注销返回未授权。
- token 对应用户不存在或已注销返回未授权。
- Service 层删除不存在用户返回 not found。

#### 状态和一致性验证

- 注销成功后 `FindUserByID` 不应返回有效用户。
- 旧 token 不能继续通过认证。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | 使用 fake Store 验证删除和 not found |
| 接口契约测试 | 是 | 验证鉴权和响应结构 |
| 模块集成测试 | 是 | 验证真实 GORM 软删除 |
| 场景测试 | 是 | 被“注销后旧 token 失效”覆盖 |
| 并发测试 | 可选 | 注销和查询并发时最终授权结果必须可解释 |
| 状态一致性测试 | 是 | 对比删除响应、后续查询和数据库软删除状态 |

## 11. Service / DAO 测试契约

### `Register`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/user/service/service.go` | `Register` |
| Store 接口 | `internal/app/user/dao/user.go` | `FindUserByAccount`、`CreateUser` |
| 单元测试 | `internal/app/user/service/service_test.go` | `fakeStore` |

#### 单元测试点

- 注册时账号去除首尾空格。
- 注册成功生成默认昵称、默认 `identity=user` 和非空 token。
- 注册时密码被哈希保存。
- 重复账号注册失败。
- 空账号、超长账号、空密码注册失败。

#### 集成测试点

- 真实数据库唯一索引阻止重复账号。
- 真实数据库保存的密码不是明文。

### `Login`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/user/service/service.go` | `Login` |
| Store 接口 | `internal/app/user/dao/user.go` | `FindUserByAccount` |
| Token | `internal/app/user/service/token.go` | `Sign` |

#### 单元测试点

- 登录成功返回 token 和用户信息。
- 登录错误密码返回未授权错误。
- 登录不存在账号返回未授权错误。

#### 集成测试点

- 使用真实数据库记录和真实密码哈希比对登录。

### `Authenticate`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/user/service/service.go` | `Authenticate` |
| Token | `internal/app/user/service/token.go` | `Verify` |
| Store 接口 | `internal/app/user/dao/user.go` | `FindUserByID` |

#### 单元测试点

- token 签名和验证。
- token 过期后认证失败。
- token 被篡改后认证失败。
- token 对应用户不存在时认证失败。

#### 集成测试点

- 真实数据库中软删除用户不能继续认证。

### `UpdateProfile`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/user/service/service.go` | `UpdateProfile` |
| Store 接口 | `internal/app/user/dao/user.go` | `UpdateUser` |

#### 单元测试点

- 修改资料时字段被去除首尾空格。
- 修改 `identity` 为 `user` 或 `merchant` 成功。
- 修改非法 `identity` 失败。

#### 集成测试点

- 真实数据库持久化资料更新。

### `DeleteMe`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/user/service/service.go` | `DeleteMe` |
| Store 接口 | `internal/app/user/dao/user.go` | `DeleteUser` |
| DAO 实现 | `internal/app/user/dao/user.go` | GORM 删除 |
| Model | `internal/app/user/model/user.go` | `DeletedAt` |

#### 单元测试点

- 注销不存在用户返回 not found。
- 注销成功后 store 不再能查询到该用户。

#### 集成测试点

- 真实数据库执行软删除。
- 软删除用户不能通过普通查询和鉴权。

## 12. 核心场景测试

### 场景 1：注册后登录并查询当前用户

#### 业务价值

这是用户模块最基础的登录态闭环。其他模块依赖该场景产出的有效用户 token。

#### 关联接口 / 方法

- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `GET /api/v1/users/me`
- `Register`
- `Login`
- `Authenticate`

#### 代码定位

- `internal/app/user/router/user.go`
- `internal/app/user/handler/user.go`
- `internal/app/user/service/service.go`
- `internal/app/user/service/token.go`
- `internal/app/user/dao/user.go`
- `internal/app/user/model/user.go`

#### 测试数据准备

- 测试批次账号不存在。
- 注册请求包含合法账号和密码。
- 账号带 `agent_user_<batch>_` 前缀。

#### Given

- 测试账号不存在。
- 测试数据库可用。

#### When

- 调用注册接口。
- 使用同一账号密码调用登录接口。
- 使用登录返回的 token 调用当前用户查询接口。

#### Then

- 注册和登录都返回 token。
- 查询接口返回的 `id`、`account`、`identity` 与登录响应一致。
- 响应中不包含 `password`.
- 数据库中密码不是明文。

#### 证据要求

- 注册 HTTP 响应。
- 登录 HTTP 响应。
- 当前用户查询 HTTP 响应。
- 数据库用户记录。
- 清理或软删除验证结果。

### 场景 2：修改资料并升级为商家身份

#### 业务价值

商品、房间等商家侧模块依赖用户身份可以变为 `merchant`。

#### 关联接口 / 方法

- `POST /api/v1/auth/register`
- `PUT /api/v1/users/me`
- `GET /api/v1/users/me`
- `UpdateProfile`

#### 代码定位

- `internal/app/user/router/user.go`
- `internal/app/user/handler/user.go`
- `internal/app/user/dto/user.go`
- `internal/app/user/service/service.go`
- `internal/app/user/dao/user.go`
- `internal/app/user/model/user.go`

#### 测试数据准备

- 已注册并登录的普通用户。
- 有效 token。
- 更新请求包含 `name`、`avatar_url`、`motto`、`identity=merchant`。

#### Given

- 已注册并登录的普通用户。

#### When

- 调用 `PUT /api/v1/users/me` 修改资料和身份。
- 再次调用 `GET /api/v1/users/me`。

#### Then

- 修改接口成功。
- 查询接口返回最新资料。
- `identity` 为 `merchant`。
- 数据库记录与查询接口一致。

#### 证据要求

- 修改 HTTP 响应。
- 当前用户查询 HTTP 响应。
- 数据库用户记录。

### 场景 3：注销后旧 token 失效

#### 业务价值

注销必须切断登录态，否则会破坏所有依赖鉴权的模块边界。

#### 关联接口 / 方法

- `POST /api/v1/auth/register`
- `DELETE /api/v1/users/me`
- `GET /api/v1/users/me`
- `DeleteMe`
- `Authenticate`

#### 代码定位

- `internal/app/user/router/user.go`
- `internal/app/user/handler/user.go`
- `internal/app/user/service/service.go`
- `internal/app/user/service/token.go`
- `internal/app/user/dao/user.go`
- `internal/app/user/model/user.go`

#### 测试数据准备

- 已注册并登录的用户。
- 已获得有效 token。

#### Given

- 已注册并登录的用户。

#### When

- 调用 `DELETE /api/v1/users/me`。
- 使用同一个 token 再次调用 `GET /api/v1/users/me`。

#### Then

- 注销接口成功。
- 后续查询返回未授权。
- 数据库中该用户为已删除状态或无法通过普通查询查到。

#### 证据要求

- 注销 HTTP 响应。
- 注销后查询 HTTP 响应。
- 数据库软删除或普通查询不可见证据。

## 13. 状态流转和一致性测试

用户模块没有显式状态机，但存在“有效用户 -> 已注销用户”的生命周期变化。

| 当前状态 | 动作 | 目标状态 | 允许 | 涉及接口 / 方法 | 一致性证据 |
| --- | --- | --- | --- | --- | --- |
| 有效用户 | 注销当前用户 | 已注销用户 | 是 | `DELETE /api/v1/users/me`、`DeleteMe` | HTTP 成功 + 数据库软删除 + 旧 token 查询失败 |
| 已注销用户 | 使用旧 token 查询当前用户 | 未授权 | 否 | `GET /api/v1/users/me`、`Authenticate` | HTTP 未授权 |

## 14. 并发测试

| 并发目标 | 是否需要 | 真实依赖 | 通过标准 |
| --- | --- | --- | --- |
| 同一账号并发注册 | 是 | 真实测试数据库唯一索引、真实 HTTP 并发请求 | 最多一个注册成功，数据库只有一个有效账号 |
| 同一用户并发资料更新 | 可选 | 真实测试数据库、真实 HTTP 并发请求 | 最终状态可解释，不出现部分字段损坏 |
| 查询当前用户与注销并发 | 可选 | 真实测试数据库、真实 HTTP 并发请求 | 注销完成后旧 token 最终不可用 |

## 15. WebSocket / Redis / 外部副作用测试

不适用。当前用户模块未看到 Redis、WebSocket、外部第三方服务或后台任务副作用。

## 16. 回归测试

| 风险 | 回归测试位置 | 触发条件 | 证据 |
| --- | --- | --- | --- |
| 响应泄露密码 | 接口契约 / 场景测试 | 注册、登录、查询当前用户 | HTTP 响应中不存在 `password` |
| 明文密码落库 | 单元 / 集成 / 场景测试 | 注册成功 | 数据库 `password` 不等于明文 |
| 重复账号创建 | 单元 / 集成 / 并发测试 | 重复注册或并发注册 | 只有一个有效用户 |
| 注销后 token 仍可用 | 单元 / 接口 / 场景测试 | 注销后查询当前用户 | 旧 token 返回未授权 |
| 非法身份写入 | 单元 / 接口测试 | 修改 `identity` 为非法值 | 返回参数错误或身份错误，数据库不变 |

## 17. 测试类型覆盖矩阵

| 测试对象 | 单元 | 接口契约 | 集成 | 场景 | 异常 | 边界 | 并发 | 状态一致性 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `POST /api/v1/auth/register` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `POST /api/v1/auth/login` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `GET /api/v1/users/me` | 是 | 是 | 是 | 是 | 是 | 否 | 可选 | 是 |
| `PUT /api/v1/users/me` | 是 | 是 | 是 | 是 | 是 | 是 | 可选 | 是 |
| `DELETE /api/v1/users/me` | 是 | 是 | 是 | 是 | 是 | 否 | 可选 | 是 |
| `account` 字段 | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `password` 字段 | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `identity` 字段 | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| 注册后登录并查询当前用户 | 否 | 是 | 是 | 是 | 否 | 否 | 否 | 是 |
| 修改资料并升级为商家身份 | 否 | 是 | 是 | 是 | 是 | 是 | 可选 | 是 |
| 注销后旧 token 失效 | 是 | 是 | 是 | 是 | 是 | 否 | 可选 | 是 |

## 18. 通过标准

**核心验证点（全部通过才算过）：**

- 注册、登录、当前用户查询、资料修改、注销接口响应结构符合统一响应约定。
- 响应中的用户信息不能包含 `password`。
- 数据库中保存的密码不能是明文。
- 重复账号不能创建两个有效用户。
- 无效 token、过期 token、篡改 token 和已注销用户 token 都不能通过认证。
- `identity` 只能是 `user` 或 `merchant`。
- 三个核心场景全部通过：注册后登录查询、修改资料升级商家、注销后旧 token 失效。

**辅助验证点（建议验证，可附说明跳过）：**

- token payload 中 subject 与用户 ID 一致。
- `created_at`、`updated_at` 字段类型和更新行为符合预期。
- 并发重复注册只有一个成功响应。

每条通过标准必须包含 HTTP 响应、数据库记录、测试命令输出或清理结果等可验证证据。

## 19. 需用户确认的问题

暂无。

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
