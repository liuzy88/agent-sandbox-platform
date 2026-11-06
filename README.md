# Agent Sandbox Platform

基于 Kubernetes SIGs 的 [Agent Sandbox](https://agent-sandbox.sigs.k8s.io/docs/getting_started/overview/) 构建。

面向 Agent 任务执行的 Kubernetes 沙箱控制平台。平台负责沙箱运行时镜像、Run 全生命周期、实时事件、人工输入、执行期引导、网络策略和结果产物；实际 Agent 逻辑由可注册的 Runtime 镜像执行。

## 能力

- 通过 REST API 创建、查询、取消 Run，并以 SSE 获取流式输出和工具事件。
- 支持人工输入、执行期 steer、文件/Skill/工具注入及结果包上传。
- 管理 Runtime 镜像的注册、校验、启用、预热池与版本化发布。
- 控制模型网关访问、临时凭据和沙箱网络边界；平台长期凭据不会下发到沙箱。
- 提供控制面、Worker、管理台和 Helm Chart，支持本地 Kubernetes、UAT 与生产部署。

## 架构

```text
Client / BFF
    │ REST + SSE
    ▼
Control Plane ── PostgreSQL / Redis / S3-compatible storage
    │
    ▼
Worker ── Agent Sandbox Controller ── Runtime Pod
                                      │
                                      └─ Model Gateway / approved tools
```

## 仓库结构

| 路径 | 说明 |
| --- | --- |
| `cmd/agent-platform` | 控制面、Worker 与初始化命令入口 |
| `internal/` | 平台领域逻辑、调度、鉴权、存储和 Kubernetes 集成 |
| `runtimes/` | Go 与 Hermes Runtime 参考实现 |
| `charts/agent-platform` | Helm Chart 与环境配置模板 |
| `docs/` | API、Runtime 契约、部署和配置文档 |
| `examples/asp-client` | 标准库 Python API 调用示例 |
| `examples/asp-demo` | FastAPI + React 集成示例 |

## 前置条件

- Go 1.26+、Docker、kubectl 和 Helm。
- Kubernetes 集群，以及已安装的 Agent Sandbox controller/extensions。
- PostgreSQL、Redis 和 S3 兼容对象存储。

安装 Agent Sandbox 扩展时，请使用与目标集群兼容的版本：

```bash
VERSION=<agent-sandbox-version>
kubectl apply -f "https://github.com/kubernetes-sigs/agent-sandbox/releases/download/${VERSION}/sandbox-with-extensions.yaml"
```

## 快速开始

下面流程用于启动本地依赖和编译服务；执行真实 Run 仍需要可访问的 Kubernetes 集群、已启用的 Runtime 镜像及网络基线配置。

```bash
# 启动 PostgreSQL、Redis、MinIO 和本地 Registry
docker compose -f docker/docker-compose.yml up -d

# 复制配置，并按本机端口修改 DATABASE_DSN、REDIS_URL、S3_* 等字段
cp .env.example .env

# 初始化 agent-platform bucket 后，编译并初始化数据库
go build -tags agentsandbox -o bin/agent-platform ./cmd/agent-platform
set -a && source .env && set +a
bin/agent-platform migrate-rebuilt

# 分别在两个终端启动控制面和 Worker
bin/agent-platform server
bin/agent-platform worker
```

本地 Compose 默认端口为 PostgreSQL `2345`、Redis `9736`、MinIO API `10009`、MinIO Console `11009`。`.env.example` 是通用模板，不能直接视为 Compose 配置；对象存储 bucket 必须预先创建。

服务启动后可检查健康状态：

```bash
curl http://127.0.0.1:8080/healthz
```

## 部署

Helm 环境配置位于 `charts/agent-platform/values-*.yaml`。复制对应模板，填写镜像、Secret、对象存储地址和集群网络 CIDR 后部署：

```bash
helm upgrade --install agent-platform charts/agent-platform \
  --namespace agent-sandbox --create-namespace \
  -f my-values.yaml
```

不要提交 API Key、数据库连接串、对象存储密钥或 Helm Secret。生产环境必须关闭 `ALLOW_INSECURE_AUTH`，并使用镜像 digest 固定控制面与 Runtime 版本。

## 文档与示例

- [对外 API 集成指南](docs/API_INTEGRATION_GUIDE.md) 与 [OpenAPI 定义](docs/openapi.yaml)
- [Runtime v1 契约](docs/RUNTIME_CONTRACT.md) 与 [Runtime 开发说明](docs/GO_RUNTIME_PLUGINS.md)
- [Helm 配置说明](charts/agent-platform/CONFIGURATION.md)
- [开发环境部署手册](docs/DEV_DEPLOYMENT.md)
- [Python 客户端示例](examples/asp-client/README.md) 和 [Web Demo](examples/asp-demo/README.md)

## 常用命令

```bash
make build-k8s              # 编译 Kubernetes 版本二进制
make admin-build            # 构建嵌入式管理台
make docker-sandbox-k8s     # 构建 Go Runtime 镜像
make docker-sandbox-hermes  # 构建 Hermes Runtime 镜像
```

## License

见 [LICENSE](LICENSE)。
