# TOS Upload Regression

测试目标：验证线上图片直传签名、TOS POST 上传、公网读图闭环。

测试类型：线上接口回归 / 真实 TOS 依赖 smoke。

测试时间：2026-06-09 04:36:02 Asia/Shanghai。

执行 agent：主 agent 串行执行。

读取文档：`docs/agent-testing/README.md`、`docs/agent-testing/templates/protocol.md`、`docs/agent-testing/guides/runner.md`、`docs/agent-testing/reports/README.md`；认证清理参考 `docs/agent-testing/modules/user.md`。

测试环境：线上后端与真实 TOS 桶；线上地址和凭据已省略。

依赖策略：连接线上公开 API 与真实 TOS；只操作本次测试批次创建的用户和对象。

测试数据：批次 `agent_tos_20260609_*`；创建 1 个测试用户；创建 1 个 PNG 测试对象，object key 已脱敏为 `uploads/images/general/<test-user>/2026/06/<test-object>.png`。

执行步骤：
1. 注册测试用户并获取测试 token。
2. 调用图片上传签名接口，请求 `image/png`、`usage=general`、1x1 PNG 大小。
3. 使用接口返回的 POST 表单上传 1x1 PNG 到 TOS。
4. 对返回的图片 URL 执行 HEAD 验证公网可读性。
5. 对同一测试对象临时设置 `public-read` ACL，验证公网可读性变化，并删除该测试对象。
6. 删除测试用户。

验证证据：
- 注册：HTTP 200，业务 `code=0`，token 已省略。
- 上传签名：HTTP 200，业务 `code=0`，包含 upload URL、form fields、`expires_in=600`。
- TOS POST 上传：HTTP 204。
- 上传后公网读图：HEAD HTTP 403。
- 临时设置测试对象 ACL 为 `public-read` 后：HEAD HTTP 200。
- 测试对象删除：TOS SDK 删除成功。
- 测试用户删除：HTTP 200，业务 `code=0`。
- 后端日志：回归期间未出现 `TOS upload signer disabled`、`invalid tos` 或 `SignImageUpload` 相关错误。

子 agent 结果摘要：未使用。

主 agent 复核结论：未使用。

冲突和处理：无。

Subagent cleanup：未使用。

并行数据隔离证明：不适用。

通过项：
- 线上 TOS 配置已生效，签名接口可生成上传表单。
- 浏览器直传路径可用，TOS 接收 POST 上传并返回 204。
- 手动设置测试对象 `public-read` ACL 后，公网图片 URL 可读。
- 测试用户已清理；测试对象已清理。

失败项：
- 当前线上已部署代码生成的 POST 表单缺少 `x-tos-acl=public-read`，导致上传对象默认私有，返回的公网图片 URL HEAD 为 403。

失败场景：上传图片后，使用接口返回的 `image_url` 读取对象。

复现步骤：注册测试用户 -> 获取图片上传签名 -> POST 上传 PNG -> HEAD 返回的图片 URL。

期望结果：公网图片 URL 返回 HTTP 200。

实际结果：公网图片 URL 返回 HTTP 403。

相关证据：TOS POST 上传 HTTP 204；同一对象设置 `public-read` ACL 后 HEAD HTTP 200。

可能原因：后端 TOS POST policy 和返回表单未包含 `x-tos-acl=public-read`。

影响范围：依赖 `image_url` 展示上传图片的前端链路会失败；单纯上传对象本身已经可用。

建议修复点：在 `internal/app/base/storage/tos_signer.go` 的 POST policy conditions 和 form fields 中加入 `x-tos-acl=public-read`，并重新构建部署后端镜像。

建议新增的回归测试：保留 `TOSPostSigner` 单元测试，断言签名条件和返回表单都包含 `x-tos-acl=public-read`；部署后再执行一次线上 smoke，验证上传后图片 URL HEAD 为 200。

跳过项：未执行完整 Apifox 对齐；本次目标是 TOS 可用性 smoke，接口结构按当前代码 DTO 构造。

Apifox 对齐偏差：未执行。

风险和建议：修复代码已在本地补充并通过 base 模块测试，但尚未构建并部署到线上；线上在新镜像发布前仍会出现上传后读图 403。

建议沉淀的回归测试：将本次 smoke runner 固化为最小线上回归脚本，输出仅包含状态码、业务码和脱敏 object key。

已知缺口：未验证 CDN、自定义域名或桶级公共读策略；本次只验证 TOS bucket 域名读写闭环。

测试数据清理结果：
- 测试批次 ID：`agent_tos_20260609_*`。
- 创建的数据：1 个测试用户，1 个 TOS 测试对象。
- 清理方式：调用用户注销接口；使用 TOS SDK 删除本次测试对象。
- 清理结果：测试用户删除成功；测试对象删除成功。
- 未清理原因：无。
