# Agent Testing Subagent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a formal subagent orchestration capability to `docs/agent-testing/` while preserving existing progressive disclosure, dependency authorization, evidence, report, and data isolation rules.

**Architecture:** Add one focused guide, `docs/agent-testing/guides/subagent.md`, then route to it from README and runner. Reports remain the final persistence layer, with new fields for subagent summaries and main-agent review.

**Tech Stack:** Markdown documentation, existing `docs/agent-testing/` contracts, `rtk`-prefixed shell commands.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `docs/agent-testing/guides/subagent.md` | New canonical protocol for when and how main agents dispatch subagents during tests. |
| `docs/agent-testing/README.md` | Routing table, directory tree, progressive-reading rules, and global hard rules for subagent tasks. |
| `docs/agent-testing/guides/runner.md` | Execution-guide integration: when to use subagents, how to split work, and how to include subagent fields in test plans. |
| `docs/agent-testing/reports/README.md` | Report schema extension for multi-agent evidence, subagent summaries, main-agent review, conflicts, and data isolation. |

The implementation is documentation-only. Do not modify module contracts, flow contracts, runner templates, or existing report files.

## Task 1: Create The Subagent Guide

**Files:**
- Create: `docs/agent-testing/guides/subagent.md`

- [ ] **Step 1: Create the guide with the protocol**

Use `apply_patch` to add `docs/agent-testing/guides/subagent.md` with this content:

````markdown
# Subagent 测试编排指南

本指南定义 agent-testing 任务中如何使用 subagent。它不定义新的业务规则，不替代模块或流程契约，只规定主 agent 与 subagent 的职责边界、授权规则、数据隔离、输出格式和汇总方式。

## 读取入口

使用 subagent 时仍然先按 `docs/agent-testing/README.md` 的渐进式读取规则进入目标任务。

常见读取顺序：

```text
单目标多 agent 编排：
docs/agent-testing/README.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/subagent.md
docs/agent-testing/modules/<module>.md 或 docs/agent-testing/flows/<flow>.md

多目标并行测试：
docs/agent-testing/README.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/subagent.md
每个 subagent 只读取自己目标需要的 modules/<module>.md 或 flows/<flow>.md

并发一致性测试涉及 subagent：
docs/agent-testing/README.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/subagent.md
docs/agent-testing/guides/concurrency-consistency.md
docs/agent-testing/guides/go-runner.md
目标模块或流程契约
```

如果计划要求读取项目上下文目录或文件，例如 `CLAUDE.md`、`docs/design/`、`docs/testing/` 或目标模块相关上下文，subagent 只能读取计划或主 agent 明确列出的范围。

## 能力定位

`subagent` 是测试执行编排能力，不是独立测试框架。

- 主 agent 是 test lead，负责读取入口、判断边界、拆分任务、控制真实依赖授权、复核证据和输出最终报告。
- subagent 是 bounded executor，只处理被分配的模块、流程、测试类型、计划草案、证据核查或报告草案。
- 测试边界仍来自 README、runner、guide、module、flow 和已批准计划。
- subagent 不能自行扩大测试范围，不能自行补齐缺失业务规则，不能把建议新增测试直接纳入本次执行范围。
- subagent 的输出是中间产物，主 agent 必须复核后才能写入最终结论。

## 适用场景

适合使用 subagent：

- 一个 module 或 flow 的测试面较宽，需要拆分计划、契约核查、执行和报告。
- 多个模块、流程或测试方向互相独立，可以并行生成计划。
- 多个模块、流程或测试方向已有明确执行许可，可以并行执行测试。
- 需要独立核查 Apifox 对齐、DTO 字段、已有报告或证据完整性。

不适合使用 subagent：

- 用户没有明确允许 subagent、委派或并行 agent 工作。
- 任务目标还不清楚，主 agent 尚未完成基本范围判断。
- 子任务之间共享同一批未隔离数据，可能相互污染。
- 下一步关键决策依赖同一个阻塞问题，主 agent 应先本地处理。
- 需要并发一致性测试但计划尚未通过人工或对话批准。

## 工作模式

### 单目标编排模式

用于一个 module 或 flow，但测试面较宽的场景。

主 agent 可以拆分为：

| 子任务 | 典型职责 |
| --- | --- |
| Plan agent | 提取契约，生成测试计划草案。 |
| Contract agent | 核查接口请求、响应、DTO 或 Apifox 对齐偏差。 |
| Execution agent | 在已授权依赖策略下执行测试并采集证据。 |
| Report agent | 整理子证据，生成报告草案。 |

主 agent 必须复核所有子输出，解决冲突，补齐失败分析，并产出唯一最终报告。

### 多目标并行模式

用于多个模块、流程或测试方向可以独立推进的场景。

拆分规则：

- 每个 subagent 负责一个明确目标，例如一个 module、一个 flow 或一个测试方向。
- 每个 subagent 只读取自己目标需要的文档和计划列出的项目上下文。
- 并行生成计划时，subagent 不得连接真实依赖。
- 并行执行测试时，计划中必须明确依赖策略、真实依赖授权和测试数据边界。
- 如果多个 subagent 连接同一个真实数据库或 Redis，主 agent 必须为每个 subagent 分配唯一子批次、数据前缀或实体集合。

## 授权模型

### 第一层：无需额外授权

subagent 可以执行：

- 读取 README、runner、目标 module 或 flow、必要 guide。
- 读取主 agent 或计划明确列出的项目上下文文件和目录。
- 分析代码、DTO、已有报告和测试契约。
- 生成测试计划、子报告草案、风险清单和建议新增测试。
- 运行本地隔离单元测试，但不得连接 MySQL、Redis、HTTP、WebSocket 或外部系统。

### 第二层：计划授权后可执行

当测试入口、目标契约或执行计划已经明确依赖策略，并且该测试类型允许真实依赖时，subagent 可以在分配范围内执行：

- 接口契约测试。
- 模块集成测试。
- 全流程测试。
- 状态一致性测试。

执行要求：

- 只能操作本批次或本 subagent 子批次测试数据。
- 并行执行时，每个 subagent 必须使用唯一 `batch_id`、名称前缀、幂等 key 前缀、Redis key 前缀或明确实体 ID 集合。
- 所有写入、状态验证和清理查询必须限定在该 subagent 的数据边界内，不能按模块、状态或时间范围批量影响其他 subagent 数据。
- 必须记录 batch id、请求响应、数据库或 Redis 证据、清理结果。
- 不得写入敏感地址、密码或可复用 token。
- 不得扩大到未分配的模块、流程或数据范围。

### 第三层：必须升级确认

以下情况 subagent 必须停止并交回主 agent：

- 计划没有注明是否连接真实依赖。
- 计划没有注明可读取的项目上下文范围，而 subagent 判断需要读取目标契约之外的上下文。
- 契约缺少业务规则、通过标准、关键状态流转或依赖策略。
- 测试需要扩大到未分配模块或流程。
- 模块文档和流程文档冲突。
- 需要执行并发一致性测试但计划还未批准。
- 需要使用线上或线上等价依赖，但测试数据边界不清楚。
- 多个 subagent 可能连接同一真实依赖，但计划没有定义唯一子批次、前缀或实体隔离策略。
- 遇到敏感信息、清理风险或非本批次数据。

## 并发一致性特殊规则

并发一致性测试不因为使用 subagent 而降低门槛。

- 先读取 `guides/concurrency-consistency.md`。
- 先把并发计划写入 `docs/agent-testing/concurrency/`。
- 计划必须包含审核状态和执行许可。
- 用户未批准前，subagent 不得连接真实数据库或 Redis、不得创建测试数据、不得启动 runner、不得发起并发请求。
- 如果用户通过对话批准，负责执行的 agent 必须先把批准记录回写到计划文件，再执行测试。
- 执行后必须把计划文件更新为 executed 并关联报告。

## Subagent Prompt 模板

主 agent 派发 subagent 时，prompt 必须自包含，至少包括：

```text
任务目标：
分配范围：
读取入口：
必须读取：
可读取项目上下文：
禁止读取或禁止扩大范围：
测试类型：
依赖策略：
真实依赖授权：未授权 | 已按计划授权 | 遇到不明确时升级
测试数据边界：
并行数据隔离策略：
输出格式：
敏感信息规则：
需要交回主 agent 的情况：
```

执行型 subagent 还必须包含：

```text
计划路径或计划摘要：
batch id 或 batch id 生成规则：
子批次、前缀或实体 ID 规则：
允许的命令范围：
清理要求：
证据要求：
```

## Subagent 输出格式

每个 subagent 必须输出：

```text
子任务：
执行状态：planned | executed | blocked | failed
读取文档：
依赖策略：
测试数据：
执行步骤：
验证证据：
通过项：
失败项：
跳过项：
阻塞项：
需要主 agent 决策：
建议下一步：
产物路径：
```

如果失败，必须补充：

```text
失败场景：
复现步骤：
期望结果：
实际结果：
相关证据：
可能原因：
建议修复点：
建议新增的回归测试：
```

## 主 Agent 汇总规则

主 agent 汇总 subagent 结果时必须：

- 标明每个 subagent 的目标、状态和产物路径。
- 区分子 agent 原始结论和主 agent 复核结论。
- 对冲突结论给出处理方式，例如重新读取契约、要求补证据或标为阻塞。
- 对未执行、跳过和阻塞项说明原因与补测条件。
- 最终报告仍按 `reports/README.md` 写入 `docs/agent-testing/reports/`。

## 失败和冲突处理

- subagent 发现业务失败时，必须提供证据，不能只输出主观判断。
- subagent 遇到阻塞时，必须说明阻塞条件、已读文档和需要决策的问题。
- 主 agent 不能把缺少证据的子结论写成最终通过。
- 如果多个 subagent 结论冲突，主 agent 必须停止自动汇总为通过，先复核契约和证据。
- 如果冲突来自测试范围扩大，按 `guides/runner.md` 的规则征得用户确认后再继续。
````

- [ ] **Step 2: Verify the file exists and has no incomplete-marker language**

Run:

```bash
rtk test -f docs/agent-testing/guides/subagent.md
rtk rg -n "TB[D]|TO[D]O|待[定]|FIXM[E]|placeholde[r]" docs/agent-testing/guides/subagent.md
```

Expected:

- `rtk test -f ...` exits `0`.
- `rtk rg ...` exits `1` with no matches.

## Task 2: Route Subagent Tasks From README

**Files:**
- Modify: `docs/agent-testing/README.md`

- [ ] **Step 1: Add subagent rows to the quick-entry table**

In `## 快速入口`, add these rows after the general execution row:

```markdown
| 使用 subagent 编排单目标测试或多目标并行测试 | `guides/runner.md` -> `guides/subagent.md`，再按目标读取契约 |
| 并行生成多个测试计划 | `guides/runner.md` -> `guides/subagent.md`，每个目标只读自己的模块或流程契约 |
| 并行执行多个已授权测试计划 | `guides/runner.md` -> `guides/subagent.md` -> 必要的执行 guide，执行前确认依赖策略、执行许可和数据隔离策略 |
```

- [ ] **Step 2: Add `subagent.md` to the directory tree**

In the `guides/` block under `## 目录结构`, include:

```text
│   ├── subagent.md
```

Place it after `runner.md` or near other execution guides.

- [ ] **Step 3: Update progressive reading rules**

In `## 渐进式读取规则`, add:

```markdown
- 使用 subagent 时，先读 `guides/runner.md`，再读 `guides/subagent.md`，然后每个 subagent 只读取自己目标需要的契约和计划列出的项目上下文。
- 并行执行测试时，如果多个 subagent 连接同一真实数据库或 Redis，计划必须先定义唯一子批次、前缀或实体隔离策略。
```

- [ ] **Step 4: Update global hard rules**

In `## 全局硬规则`, add:

```markdown
- Subagent 的输出只是中间产物，最终通过、失败和风险结论必须由主 agent 复核后写入报告。
- 多个 subagent 连接同一真实依赖时，必须使用互不重叠的批次 ID、名称前缀、幂等 key、Redis key 或实体 ID 集合，禁止相互污染测试数据。
```

- [ ] **Step 5: Verify README routing mentions subagent**

Run:

```bash
rtk rg -n "subagent|Subagent|并行执行|数据隔离" docs/agent-testing/README.md
```

Expected: matches in the quick-entry table, progressive-reading rules, and global hard rules.

## Task 3: Integrate Subagent Rules Into Runner

**Files:**
- Modify: `docs/agent-testing/guides/runner.md`

- [ ] **Step 1: Add subagent execution entries**

In the `## 执行入口` code block, add:

```text
使用 subagent 编排单目标测试：
读取 docs/agent-testing/README.md
读取 docs/agent-testing/guides/runner.md
读取 docs/agent-testing/guides/subagent.md
读取目标 docs/agent-testing/modules/<module>.md 或 docs/agent-testing/flows/<flow>.md

使用 subagent 并行测试多个目标：
读取 docs/agent-testing/README.md
读取 docs/agent-testing/guides/runner.md
读取 docs/agent-testing/guides/subagent.md
每个 subagent 只读取自己目标需要的模块或流程契约
```

- [ ] **Step 2: Add subagent fields to the required test plan**

In `### 3. 生成测试计划`, extend the plan block with:

```text
是否使用 subagent：
主 agent 职责：
subagent 分工：
可读取项目上下文：
真实依赖授权：
并行数据隔离策略：
```

Then add this paragraph after the plan block:

```markdown
如果使用 subagent，测试计划必须写清主 agent 与每个 subagent 的分工、每个 subagent 可读取的项目上下文、真实依赖授权状态，以及并行执行时的唯一子批次、前缀或实体隔离策略。计划没有写清时，subagent 只能生成计划或提出阻塞项，不得连接真实依赖。
```

- [ ] **Step 3: Add a dedicated subagent section**

Add this section after `### 4. 选择依赖策略` and before `#### 单元测试`:

```markdown
### 5. 使用 Subagent

当任务需要单目标多 agent 编排或多目标并行测试时，agent 必须读取 `docs/agent-testing/guides/subagent.md`。

主 agent 负责：

- 判断是否适合使用 subagent。
- 拆分明确、互不重叠的子任务。
- 为每个 subagent 指定读取入口、必须读取文档、可读取项目上下文和禁止扩大范围。
- 明确真实依赖授权状态。
- 并行执行时分配唯一子批次、数据前缀、幂等 key 前缀、Redis key 前缀或实体 ID 集合。
- 复核 subagent 输出并写入最终报告。

subagent 负责：

- 只处理分配范围内的目标。
- 按渐进式读取规则读取最少必要文档。
- 按计划限制读取项目上下文。
- 在授权不明确、契约缺失、数据隔离不清或清理风险出现时停止并升级。
- 输出 `guides/subagent.md` 规定的结构化结果。

如果多个 subagent 连接同一真实数据库或 Redis，禁止共享未隔离测试数据。所有写入、验证和清理查询必须限定在各自子批次、前缀或实体 ID 集合内。
```

- [ ] **Step 4: Renumber following sections only if needed**

If the document already has later numbered `###` sections that become confusing after adding `### 5. 使用 Subagent`, rename the new heading to:

```markdown
### 使用 Subagent
```

Do not rewrite unrelated sections.

- [ ] **Step 5: Verify runner has all required phrases**

Run:

```bash
rtk rg -n "guides/subagent.md|是否使用 subagent|subagent 分工|并行数据隔离策略|使用 Subagent" docs/agent-testing/guides/runner.md
```

Expected: all terms match.

## Task 4: Extend Report Rules For Multi-Agent Results

**Files:**
- Modify: `docs/agent-testing/reports/README.md`

- [ ] **Step 1: Extend required report fields**

In the `## 报告内容` required field block, add these fields after `执行 agent：` or after `验证证据：`:

```text
子 agent 结果摘要：（使用 subagent 时填写；未使用写"未使用"）
主 agent 复核结论：（使用 subagent 时填写；未使用写"未使用"）
冲突和处理：（使用 subagent 时填写；无冲突写"无"）
并行数据隔离证明：（并行执行且连接真实依赖时填写；不适用写"不适用"）
```

- [ ] **Step 2: Add writing requirements for subagent reports**

In `填写要求：`, add:

```markdown
- 使用 subagent 时，`执行 agent` 必须区分主 agent 和子 agent；子 agent 的输出必须作为证据或摘要记录，不能直接替代主 agent 的最终结论。
- `主 agent 复核结论` 必须说明哪些子结论已复核、哪些仍为建议或阻塞。
- `冲突和处理` 必须记录子 agent 结论冲突、范围冲突、证据不足或数据隔离问题，以及主 agent 的处理方式。
- 多个 subagent 并行连接真实数据库或 Redis 时，`并行数据隔离证明` 必须记录每个 subagent 的 batch id、数据前缀、幂等 key 前缀、Redis key 前缀或实体 ID 集合。
```

- [ ] **Step 3: Extend cleanup requirements**

In `## 测试数据清理`, after the current online dependency paragraph, add:

````markdown
如果多个 subagent 并行执行测试，报告必须按 subagent 记录：

```text
subagent：
测试批次 ID / 子批次 ID：
数据前缀或实体 ID 集合：
清理方式：
清理结果：
未清理原因：
```

任何清理动作都必须限定在对应 subagent 的数据边界内。
````

- [ ] **Step 4: Extend the report template basic info**

In `## 报告模板`, under `## 基本信息`, keep the existing `执行 agent：` line and add:

```markdown
- 主 agent：
- 子 agent：
- 子 agent 结果摘要：
- 主 agent 复核结论：
- 冲突和处理：
- 并行数据隔离证明：
```

- [ ] **Step 5: Verify report README mentions the new fields**

Run:

```bash
rtk rg -n "子 agent 结果摘要|主 agent 复核结论|冲突和处理|并行数据隔离证明|子批次" docs/agent-testing/reports/README.md
```

Expected: all terms match.

## Task 5: Cross-Document Verification

**Files:**
- Read: `docs/agent-testing/README.md`
- Read: `docs/agent-testing/guides/runner.md`
- Read: `docs/agent-testing/guides/subagent.md`
- Read: `docs/agent-testing/reports/README.md`

- [ ] **Step 1: Verify there are no incomplete markers**

Run:

```bash
rtk rg -n "TB[D]|TO[D]O|待[定]|FIXM[E]|placeholde[r]" docs/agent-testing/README.md docs/agent-testing/guides/runner.md docs/agent-testing/guides/subagent.md docs/agent-testing/reports/README.md
```

Expected: exit `1` with no matches.

- [ ] **Step 2: Verify routing consistency**

Run:

```bash
rtk rg -n "guides/subagent.md" docs/agent-testing/README.md docs/agent-testing/guides/runner.md
```

Expected: both README and runner reference `guides/subagent.md`.

- [ ] **Step 3: Verify data isolation consistency**

Run:

```bash
rtk rg -n "子批次|数据隔离|数据前缀|实体 ID|相互污染|并行数据隔离" docs/agent-testing/README.md docs/agent-testing/guides/runner.md docs/agent-testing/guides/subagent.md docs/agent-testing/reports/README.md
```

Expected: matches in all four files.

- [ ] **Step 4: Verify concurrency approval is still explicit**

Run:

```bash
rtk rg -n "并发一致性|批准|执行许可|不得连接真实" docs/agent-testing/guides/subagent.md docs/agent-testing/guides/runner.md docs/agent-testing/README.md
```

Expected: output shows that subagent usage does not bypass concurrency plan approval.

- [ ] **Step 5: Review diff scope**

Run:

```bash
rtk git diff -- docs/agent-testing/README.md docs/agent-testing/guides/runner.md docs/agent-testing/guides/subagent.md docs/agent-testing/reports/README.md
```

Expected: only the four intended documentation files changed for this implementation.

## Task 6: Commit The Documentation Change

**Files:**
- Stage: `docs/agent-testing/README.md`
- Stage: `docs/agent-testing/guides/runner.md`
- Stage: `docs/agent-testing/guides/subagent.md`
- Stage: `docs/agent-testing/reports/README.md`

- [ ] **Step 1: Check existing dirty worktree before staging**

Run:

```bash
rtk git status --short
```

Expected: existing unrelated user changes may be present. Do not stage unrelated files.

- [ ] **Step 2: Stage only implementation files**

Run:

```bash
rtk git add docs/agent-testing/README.md docs/agent-testing/guides/runner.md docs/agent-testing/guides/subagent.md docs/agent-testing/reports/README.md
```

Expected: only these four docs are staged for this implementation.

- [ ] **Step 3: Confirm staged scope**

Run:

```bash
rtk git diff --cached --name-status
```

Expected:

```text
M	docs/agent-testing/README.md
M	docs/agent-testing/guides/runner.md
A	docs/agent-testing/guides/subagent.md
M	docs/agent-testing/reports/README.md
```

- [ ] **Step 4: Commit**

Run:

```bash
rtk git commit -m "docs(agent-testing): add subagent orchestration guide"
```

Expected: commit succeeds.
