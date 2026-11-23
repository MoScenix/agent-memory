# Agent Memory 服务

| 目录 | 职责 |
| --- | --- |
| `biz/handler` | Hertz 请求处理和参数绑定 |
| `biz/router` | Hertz 路由注册和中间件 |
| `biz/service` | 每个接口的服务实现入口 |
| `biz/model` | GORM 表模型和查询 |
| `biz/dal/mysql` | MySQL 与 GORM 初始化 |
| `biz/worker` | Outbox 领取、重试和超时回收 |
| `biz/dal/milvus` | Milvus 初始化、Eino Indexer 与混合检索 |
| `biz/utils/embedding.go` | Qwen Embedding 初始化 |
| `biz/utils` | HTTP 响应工具 |
| `hertz_gen` | cwgo 生成的 Protobuf 模型 |
| `conf` | dev、test、online 配置 |
| `script` | 构建产物的启动脚本 |

在项目根目录执行 `make gen svc=agent_memory` 更新生成代码，执行 `make test` 验证服务，执行 `make run svc=agent_memory` 启动。

新环境先执行 `make env-start` 启动中间件，再执行 `make migrate` 创建表和外键。HTTP 请求的 POST JSON 直接使用接口的 `Body` 字段，字段名使用 snake_case。业务时间使用 RFC 3339，响应中的 64 位版本号按 Protobuf JSON 规则使用十进制字符串。

旧库升级时先停止旧版 HTTP 服务，再执行 `make migrate`，最后启动新版。迁移把旧边的 `to_node_id` 重命名为 `node_id`，把 `from_node_id` 重命名为 `previous_node_id`，保留已有行、变更 ID 和约束。对外响应的 `from_node_id` 现在指新节点，`to_node_id` 指旧节点。

节点历史默认每页 20 条，最多 100 条。事实变更一次最多处理 100 个旧节点。检索默认取 10 条，最多 100 条，默认使用混合检索。超过 4096 个已同步候选时返回 HTTP 422。配置 Embedding 密钥后，HTTP 服务会连接 Milvus 提供检索。

关系追溯接受 `max_depth` 或 `end_time`。时间模式可同时传公共上界 `start_time`，所有起点共享 `[end_time, start_time)`。事实检索接受 `time_filter.valid_at` 或 `time_filter.valid_range`。后者用 `start` 和 `end` 指定半开区间，返回业务有效期与它相交的候选。

## 启动 Outbox Worker

Worker 与 Hertz 独立运行，需要已建好 MySQL 表以及 Milvus 2.6。它启动时创建并加载配置中的 Milvus 集合及两套索引。已有集合会校验字段、维度和 BM25 函数。若集合还是原来的纯向量结构，可配置新集合名，然后从 MySQL 重新同步历史节点。

设置 `DASHSCOPE_API_KEY` 或 `AGENT_MEMORY_EMBEDDING_API_KEY`。默认使用通义 `text-embedding-v4`，1024 维。按服务地域通过 `AGENT_MEMORY_EMBEDDING_BASE_URL` 配置 DashScope 兼容 API 的基础地址，路径到 `/compatible-mode/v1`。然后在仓库根目录运行以下命令。 [通义 Embedding API 文档](https://www.alibabacloud.com/help/en/model-studio/embedding-interfaces-compatible-with-openai)

```sh
make run-outbox
```

环境默认是 `dev`，由 `GO_ENV` 切换。默认每秒轮询，单次处理限时 30 秒，领取租约为 60 秒，最多领取 5 次。参数在各环境的 `outbox` 配置段。消费同一张表的 Worker 使用同一个 Embedding 模型和 Milvus 集合。
