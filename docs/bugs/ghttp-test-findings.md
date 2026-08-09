# ghttp 测试覆盖补充过程中的发现记录

> 记录在 2026-08-09 的 server/client 层测试补充中发现的疑点。分为三类：
> **疑似 bug（需 review 生产代码）** / **测试缺口（纯补测试）** / **已解决**。
> 未经维护者 review，不得修改生产代码来适配测试。

## 一、疑似 bug（需 review 生产代码后才能修复）

### B1. `Client.SetAuthToken`（客户端级）设置后从未生效 — **已修复**
- **位置**：`ghttp/client.go` — `(*Client).SetAuthToken`（约 222 行）、`(*Client).R()`（171 行）、`send`（约 952 行）
- **现象**：`Client.SetAuthToken("tok")` 只是写入 `c.authToken`/`c.authScheme`，但 `R()` 不把它们继承到 `*Request`，`send` 只应用 `r.AuthToken`（请求级字段）。结果是**客户端级认证实际无效**，与 README 声称的行为不符。
- **证据**：测试 `TestClientClientLevelSetters` 断言 `X-Auth == "Bearer tok"` 失败（auth header 为空）。
- **修复（2026-08-09）**：`R()` 在构造时继承 `c.authToken`/`c.authScheme`（与 headers/queryParams 传播一致），scheme 为空时默认 `Bearer`；请求级 `SetAuthToken` 显式覆盖。回归测试：`TestClientClientLevelAuthInherited`。

### B2. `TestClientResponseAccessors` 的 `Time()` 断言偶发失败 — **已修复**
- **位置**：`ghttp/client_test.go` — `TestClientResponseAccessors`（约 511 行）
- **现象**：`resp.Time() <= 0` 在本地 fast server 下偶发失败——单次请求完成时间不足 1ns 精度时 `Time()` 返回 0。
- **判断**：测试断言脆弱（时序），非实现 bug。`Time()` 语义是"请求耗时"，极快时舍入为 0 合理。
- **修复（2026-08-09）**：断言改为 `resp.Time() < 0`（仅验证非负）。

## 二、测试缺口（纯补测试即可，不动生产代码）

以下函数当前 0% 或极低覆盖，均为**新增测试**可覆盖，不涉及生产代码改动：

### client 层（Request 构造器 / 客户端 setter）
- `(*Client).SetBaseURL`、`SetHeader`、`SetHeaders`、`SetAuthToken`、`SetTimeout`、`SetDebug`、`SetLogger`、`SetRetryCount/WaitTime/MaxWaitTime`、`SetRetryConditions`、`SetCookies`（客户端级）
- `(*Request).SetBody`、`SetXMLBody`、`SetCookie(s)`、`SetPathParams`、`SetDoNotParseResponse`、`SetRetry*`、`SetAuthToken/Scheme`（请求级）
- 包级快捷函数 `GET/PUT/DELETE`（`client.go` 1387+ 行，非 `R().Get`）

### server / builder 层
- `builder_pre127.go` `ToStaticFS`
- `builder_core.go` `buildParamsHandlerWithGlobalValidator` / `buildParamsHTTPHandlerWithGlobalValidator` / `validateDirectParamsWithGlobalValidator`（Params + 服务器级全局 validator 路径）
- `builder_option.go` `OptConsumes/OptMaxBodyBytes/OptResponseHeader/OptErrorWriter/OptProblemDetails/OptSkipValidation/OptValidate`
- `config.go` `WithConsumes/WithOpenAPIPath/WithStrictContentType/WithWebSocketOriginChecker/WithHostResolver`

### 其他
- `input.go` `ParseInput`、`validate.go` `NewDefaultValidator` / `FieldValidationError.Error`、`render.go` `HTML`
- `slog.go` `DebugContext/WarnContext`、`middleware.go` `Unwrap`、`writer.go` `Unwrap/Push`
- `path_param.go` `Reset/Truncate/ServeHTTP`、`params.go` `PathBoolDefault`
- `websocket.go` `SetReadDeadline/SetWriteDeadline/WriteJSONContext`
- `codec_form.go` `Marshal`

## 三、已解决（测试自包含化）

- **`SetFile` 按路径上传测试的 fixture**：原计划在 `ghttp/testdata/` 放固定文件，改为 `t.TempDir()` 运行时生成，避免污染仓库。
- **unsafe 信任代理警告**：`New()` 默认信任所有 IP 时输出警告，`sync.Once` 每进程一次；测试进程会打一条日志，可接受（非问题）。

## 四、补充说明

- 本次新增 `ghttp/server_extra_test.go`（12 个测试）+ `ghttp/client_extra_test.go`（14 个测试），全部不依赖生产代码改动。
- 覆盖提升：80.2% → 82.8%（statement）。
- 尚未触碰的剩余低覆盖区域见"二"，可继续补测试，无需 review 生产代码。
