# web2api

`web2api` 是一个将 Web 原生 AI 服务转换为 OpenAI 兼容接口的 Go 平台。

它通过 WASM 插件适配不同上游站点，平台本身负责账号管理、请求路由、HTTP 执行、工具调用兼容转换，以及管理后台能力。

[English](../README.md) | 简体中文

## 平台能力

- 提供 OpenAI 风格接口：`/v1/models`、`/v1/completions`、`/v1/responses`、`/v1/chat/completions`
- 支持基于 WASM 的插件扩展，不同网站适配逻辑与平台解耦
- 平台统一管理账号、客户端访问、模型目录与路由
- 支持 `chat.completions` 的流式与非流式返回
- 支持平台侧工具调用提示词注入与 OpenAI 风格 `tool_calls` 输出
- 提供管理后台，用于插件、账号、客户端、运行池、工具调用日志和在线测试

## 已支持插件

- `grok-web`：可将网页 Grok 转换为 OpenAI 兼容标准接口

## 适合的场景

- 把网页 AI 服务统一包装成标准 API
- 管理多个上游来源和多个账号池
- 为插件开发提供统一运行时和宿主 HTTP 能力
- 在兼容 OpenAI 客户端的同时保留上游特性

## 系统组成

### 平台层

- 请求入口与 OpenAI 接口兼容
- 账号和客户端鉴权
- 插件调度与模型映射
- 宿主 HTTP 调用和 continue/resume 运行循环
- 工具调用桥接与流式输出整形

### 插件层

- 将上游网站协议转换为平台 ABI
- 声明模型与账号字段
- 解析上游返回并提供标准化结果

### 管理界面

- `/admin/plugins`
- `/admin/accounts`
- `/admin/clients`
- `/admin/test`
- `/admin/runtime`
- `/admin/tool-calls`


### 管理总览

![Admin Overview](assets/admin-overview.png)

### 插件运行日志

![Tool Call Logs](assets/admin-runtime.png)

### 用户测试页

![WebUI Test](assets/webui-test.png)

## 快速启动

```bash
go mod tidy
go run ./cmd/web2api
```

访问：

- `http://localhost:8080/admin`
- `http://localhost:8080/webui`
- `http://localhost:8080/webui/test`

## 开发文档

- 插件规范：`docs/plugin-spec.md`
- 插件开发：`docs/plugin-dev.md`
- AI 插件编写指南：`docs/plugin-ai-guide.md`
- 插件生成提示模板：`docs/plugin-prompt-template.md`
- 插件检查清单：`docs/plugin-checklist.md`
