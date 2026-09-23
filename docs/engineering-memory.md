# 工程记忆 (Engineering Memory)

## 2026-09-23 | 全仓库 | 双方重复实现同一功能导致 merge 后 ent 生成代码 redeclared

- **症状**: 合并官方 unstable 后 `go build ./...` 报 `internal/ent/requestexecution/requestexecution.go: FieldResponseHeaders redeclared in this block`、`where.go: ResponseHeadersIsNil redeclared`。
- **触发条件**: 本地分支与上游各自独立实现了同一功能（本例：请求响应头持久化，官方 PR #2502），手工解决冲突时对 schema/业务文件采用了"双方都保留"策略。
- **根因**: `internal/ent/schema/request_execution.go` 中 `response_headers` 字段被保留两份定义；`internal/server/biz/request.go` 中 `UpdateRequestExecutionResponseHeaders` 方法重复定义。ent 生成代码（`internal/ent/**`、`internal/server/gql/generated.go`）是生成物，冲突时应先解决 schema 源文件，再用 `make generate` 整体重生成，禁止手工逐块合并生成代码。
- **最终修复**: schema 删除本地重复字段保留官方版；biz 删除无脱敏的本地重复方法保留带脱敏/存储策略的官方版；`make generate` 重生成全部 ent/graphql 代码。另在 `orchestrator/request_execution.go` 的 `OnOutboundRawResponse` 恢复本地丢失的 `state.ResponseStatusCode = response.StatusCode` 赋值（执行日志状态码记录为本地功能，上游无对应实现）。
- **回归验证**: `go build ./...`、`go test ./internal/server/gc/... ./internal/server/biz/... ./internal/server/backup/... ./internal/server/orchestrator/... ./internal/server/middleware/...`、`cd llm && go test ./httpclient/...`、`cd frontend && npx tsc --noEmit` 全部通过。
- **适用环境**: 任何与上游 diverge 的功能文件（尤其 ent schema 及全部生成代码）。
- **已排除的无效方案**: 手工编辑 `internal/ent/**` 生成文件去重（治标不治本，schema 源仍会再产出重复）。

**教训**: 解决 ent 生成代码冲突的唯一正确路径 = 解决 schema/gql 源文件冲突 → `make generate` → 编译验证。解决"双方新增同名字段/方法"冲突时必须先 diff 双方语义再择一，而非并存。

## 2026-09-23 | frontend | 冲突误删 pnpm.overrides 导致 Docker 构建 frozen-lockfile 失败

- **症状**: `./deploy.sh --build` 在 Docker 层 `pnpm install --frozen-lockfile` 报 `ERR_PNPM_LOCKFILE_CONFIG_MISMATCH: current "overrides" configuration doesn't match the value found in the lockfile`。
- **触发条件**: 同一场合并冲突中手工解决 `frontend/package.json` 时删除了官方新增的顶层 `"pnpm": { "overrides": { "uuid@>=13.0.0 <14.0.0": "14.0.0" } }` 块，而 `frontend/pnpm-lock.yaml`（取自官方侧）仍携带 `overrides:` 段。
- **根因**: pnpm frozen 模式严格校验 package.json 的 overrides 与 lockfile 顶层 overrides 一致；冲突解决丢失其一即失败。
- **最终修复**: 从 `git show MERGE_HEAD:frontend/package.json` 恢复该块（`git diff MERGE_HEAD -- frontend/package.json` 输出为空即对齐）。
- **回归验证**: `cd frontend && pnpm install --frozen-lockfile` 通过；`./deploy.sh --build` 完整构建成功。
- **适用环境**: pnpm 10 + `--frozen-lockfile` 的 Docker 构建（Dockerfile frontend-builder 层）。
- **已排除的无效方案**: 只删 lockfile 的 overrides 段（破坏官方锁定的 uuid 版本约束）；`--no-frozen-lockfile` 重建 lockfile（引入无关依赖漂移）。

## 2026-09-23 | frontend | 合并后运行时 ReferenceError——tsc 检查入口错误的陷阱

- **症状**: 页面运行时 `ReferenceError: Key is not defined` / `expandRevision is not defined`，但 `npx tsc --noEmit` 显示绿色、vite build 成功。
- **触发条件**: 同一场 merge 冲突解决，冲突双方对同一组件各自新增 props/图标（如 CollapseBlock 的 `expandRevision`、lucide `Key`），解决时签名取了一侧、函数体用了另一侧。
- **根因（检查工具层）**: ① `frontend/tsconfig.json` 是 solution-style（仅 `references`），`npx tsc --noEmit` 在根配置上**空转不检查任何文件**；正确入口是 `npx tsc -b`。② vite build 用 esbuild 转译，不做符号解析，未定义标识符不报错。两者叠加造成"语法检查都过了"的假象。
- **最终修复**: `tsc -b` 全量扫描 + 与官方基线（`git worktree add /tmp/axb MERGE_HEAD` + 软链 node_modules）对比错误集，修复全部本次引入错误：补 `Key` 导入、CollapseBlock/ToolCallCard/ToolResultCard 补 `expandAll/expandRevision` props 及调用点传参、`previewGcCleanup` 局部 mutation 变量遮蔽同名导入函数（删除 mutation 直接用带 AbortSignal 的导入函数）、i18next `count` 为复数保留字须为 number（冲突解决传了格式化字符串，改插值变量名 `count`→`num` 并同步 locales）、`reconnectTimer` 类型 `Timeout`→`number`、`X-Project-ID` null→`?? ''`、删除合并残留死变量。
- **回归验证**: `npx tsc -b` 错误集为官方基线的严格子集（零新增）、TS2304/TS2552 清零、`npx vite build` 通过、`npm run test:unit` 154/154 通过。
- **适用环境**: 任何与上游 diverge 的前端冲突解决后验证。官方 unstable 本身带 ~191 个存量 tsc 错误（ai-elements、quota-badges 等），因此**禁止以"tsc 全绿"为门禁**，必须与基线 diff。
- **经验**: i18next `t(key, {count})` 中 `count` 是保留复数选项，只接受 number；展示已格式化的数字必须用其他变量名。

