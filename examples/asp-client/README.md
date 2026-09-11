# Agent Sandbox Platform Python 客户端示例

`client.py` 是一个只依赖 Python 标准库的命令行示例客户端，用于调用 Agent Platform 的 V1 HTTP API。它用于验证和演示平台集成，不是正式 SDK。

## 前置条件

- Python 3.10 或更高版本。
- 可访问的 Agent Platform 服务地址。
- 已创建的客户端 API Key 和运行时镜像 ID。
- 使用 `run` 命令上传文件、技能或下载结果时，还需要兼容 S3 的对象存储凭据。

通过环境变量配置常用参数：

```bash
export AGENT_PLATFORM_BASE_URL=http://localhost:30080
export CLIENT_API_KEY='your-client-api-key'
export AGENT_PLATFORM_IMAGE_ID='your-runtime-image-id'
export MODEL_NAME='your-model-name'
export MODEL_BASE_URL='https://model.example.com/v1'
export MODEL_ACCESS_API_KEY='your-model-api-key'
```

命令行参数优先于环境变量。可运行 `python3 client.py --help` 查看完整选项。

## 常用命令

创建一个 Run：

```bash
python3 client.py create --prompt '请简要介绍这个项目'
```

创建 Run、持续读取事件，并在成功后下载和校验结果包：

```bash
export S3_ENDPOINT='minio.example.com:9000'
export S3_BUCKET='agent-platform'
export S3_ACCESS_KEY_ID='your-access-key'
export S3_SECRET_ACCESS_KEY='your-secret-key'
export S3_USE_SSL=false

python3 client.py run \
  --prompt '分析附件中的数据' \
  --file ./input.csv \
  --output-dir ./agent-platform-output
```

查询或取消已创建的 Run：

```bash
python3 client.py get <run-id>
python3 client.py events <run-id>
python3 client.py cancel <run-id>
```

当事件流返回 `agent.request_input` 时，提交人工输入：

```bash
python3 client.py answer <run-id> <input-id> --answer '{"approved":true}'
```

向运行中的 Agent 发送实时引导：

```bash
python3 client.py steer <run-id> --message '请优先给出风险和结论。'
```

## S3 注意事项

`run` 命令会为输入文件、技能包和结果包生成 S3 SigV4 预签名 URL。对象存储端点必须能被本机和平台运行时访问；若使用 MinIO，请根据部署方式正确设置 `S3_ENDPOINT` 与 `S3_USE_SSL`。

脚本会在下载结果包后验证平台返回的 SHA-256 校验值。不要将 API Key 或对象存储密钥写入该目录的文件或提交至仓库。
