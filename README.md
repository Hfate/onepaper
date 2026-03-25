# onepaper

个人可用的「论文抓取 → AI 筛选 → 中文科普成稿 → 配图 → 公众号 HTML → 微信草稿/发布」流水线，Go 实现，模块解耦，便于长期维护与扩展。

## 功能概览

1. **crawler**：arXiv Atom API 拉取最近 N 小时内论文（默认 Top 20）；Nature / Science / Lancet 预留接口（`journals_stub.go`）。
2. **filter**：OpenAI 兼容 API，对 title+abstract 打分（JSON），先筛后写长文以控费。
3. **summarizer**：生成公众号风格中文长文（JSON 结构），`RenderHTML` 输出 inline CSS 的微信图文 HTML。
4. **image**：优先 arXiv 页面抓图，过滤小于 500×500；失败则 PDF 首页抽最大嵌入图（pdfcpu）。
5. **publisher**：`silenceper/wechat` 上传正文图、缩略图永久素材，新建草稿；可选 `freepublish` 发布。
6. **scheduler**：`robfig/cron` 可配置时间与 cron 表达式。
7. **repository**：MySQL 存 papers / articles（可选）。

## 目录结构

```
cmd/server/           # main
config/               # 配置加载
internal/crawler/
internal/filter/
internal/summarizer/
internal/image/
internal/publisher/
internal/scheduler/
internal/repository/
internal/model/
pkg/ai/
pkg/logger/
migrations/
Dockerfile
docker-compose.yml
docker-compose.local-mysql.yml
```

## 快速开始（本机 Go）

### 1. 准备 MySQL（可选）

```bash
mysql -u root -p < migrations/001_schema.sql
```

### 2. 配置

```bash
cp config.example.yaml config.yaml
# 编辑 database.dsn、ai（base_url / 模型 / api_key 或环境变量 AI_API_KEY 等）、wechat
export AI_API_KEY=sk-...   # 或 OPENROUTER_API_KEY / DEEPSEEK_API_KEY / OPENAI_API_KEY
```

### 3. 运行

```bash
go run ./cmd/server -config config.yaml
```

- 定时任务：`scheduler.enabled: true` 时按 cron 执行（默认每天 9:00，上海时区）。
- 立即跑一轮（调试用）：

```bash
go run ./cmd/server -config config.yaml -run-once
```

- HTTP：`GET /healthz` 健康检查。

### LLM 供应商（OpenAI / OpenRouter / DeepSeek）

使用同一套 **Chat Completions** 客户端：`config.yaml` 里配置 `ai.base_url`（必须以 `/v1` 结尾，例如 `https://api.deepseek.com/v1`、`https://openrouter.ai/api/v1`）、`ai.model` / `score_model` / `article_model`，以及 `ai.api_key` 或占位符。

**API Key** 可来自环境变量（在 yaml 里写 `${AI_API_KEY}`，或 yaml 留空由程序按序读取）：**`AI_API_KEY`** → `LLM_API_KEY` → **`OPENROUTER_API_KEY`** → **`DEEPSEEK_API_KEY`** → `OPENAI_API_KEY`。若 yaml 未写 **`base_url`**，可读 **`AI_BASE_URL`**。

**OpenRouter**：可在 `.env` 设置 `AI_BASE_URL=https://openrouter.ai/api/v1`，`OPENROUTER_API_KEY=...`，模型如 `openai/gpt-4o-mini`；可选用 `OPENROUTER_HTTP_REFERER`、`OPENROUTER_X_TITLE`（见 OpenRouter 文档）。

**DeepSeek**：`base_url` 一般为 `https://api.deepseek.com/v1`，`DEEPSEEK_API_KEY`，模型如 `deepseek-chat`。

JSON 输出依赖供应商对 `response_format` 的支持；若某模型不支持，需在 `filter`/`summarizer` 中改提示或关闭强制 JSON（后续可按需扩展）。

---

## Docker 部署

默认 **`docker compose`**（`docker-compose.yml`）**只启动 onepaper**，并把它接到**已有的** MySQL 所在 Docker 网络（典型场景：与 **slowwindow** 同机的 `slowwindow-mysql` 复用，新建库 **`onepaper`**）。对方 MySQL **未映射宿主机端口**也没问题，容器间走内网即可。

### 复用其它项目的 MySQL（推荐）

1. **查网络名**（对方 `docker-compose.yml` 里 `networks` 下的 key 为 `slowwindow-network` 时，实际名字多为 **`目录名_slowwindow-network`**）：

   ```bash
   docker network ls
   ```

   将结果写入本仓库 `.env` 的 **`EXTERNAL_MYSQL_NETWORK`**（默认示例：`slowwindow_slowwindow-network`，不对则必改）。

2. **建库与表（一次性）**  
   在宿主机执行（密码与 slowwindow `.env` 里 root 一致，`001_schema.sql` 会 `CREATE DATABASE onepaper` 并建表）：

   ```bash
   docker exec -i slowwindow-mysql mysql -uroot -p"$DB_ROOT_PASSWORD" < migrations/001_schema.sql
   ```

   若更愿意复用业务账号 `${DB_USER}`，在 MySQL 里执行 `GRANT ALL ON onepaper.* TO '${DB_USER}'@'%'; FLUSH PRIVILEGES;` 后，把 `config.yaml` 里 dsn 改成 `${DB_USER}:${DB_PASSWORD}@tcp(slowwindow-mysql:3306)/onepaper?...`（见 `config.docker.example.yaml` 注释）。

3. **配置**  
   `cp config.docker.example.yaml config.yaml`，确认 dsn 里主机为 **`slowwindow-mysql`**（与对方 `container_name` 一致；若对方改成别名/服务名，则同步改这里）。

4. **环境变量**  
   `cp .env.example .env`：填写 **LLM**（如 **`AI_API_KEY`**，或 `OPENROUTER_API_KEY` / `DEEPSEEK_API_KEY`）、**`EXTERNAL_MYSQL_NETWORK`**、**`DB_ROOT_PASSWORD`**（与 slowwindow 一致）；OpenRouter 可设 **`OPENROUTER_HTTP_REFERER`**。

5. **启动**

   ```bash
   docker compose build --no-cache   # 首次或依赖大变时
   docker compose up -d
   ```

   日志：`docker compose logs -f onepaper`  
   停止（不删 `onepaper-data` 卷）：`docker compose down`

**原理**：onepaper 通过 **`external: true`** 加入对方栈已创建的 bridge 网络，DNS 可解析 `slowwindow-mysql`。不需要再给 onepaper 单独起一个 MySQL，也**不会**占用第二个 3306 宿主机映射（对方 MySQL 本身也可不暴露端口）。

### 独立 MySQL（不共用 slowwindow）

使用 **`docker-compose.local-mysql.yml`**（自带 MySQL，数据默认 `./data/mysql`）：

```bash
docker compose -f docker-compose.local-mysql.yml up -d --build
```

此时 `config.yaml` 里 dsn 主机应改为 **`mysql`**，root 密码与 `.env` 里 **`MYSQL_ROOT_PASSWORD`**（可配合容器内 **`DB_ROOT_PASSWORD`**）一致。

### 快速更新

```bash
git pull
docker compose build
docker compose up -d
```

（独立 MySQL 时把 `docker compose` 换成带 `-f docker-compose.local-mysql.yml` 的同序命令。）

- **`onepaper-data`**：配图等命名卷；`docker compose down` 默认**不删除**。
- **复用外部 MySQL**：数据仍在对方 `/data/mysql`（或其卷）中，与本项目卷无关。

### 端口与环境变量

- **`9090:8080`**：宿主机访问 onepaper 健康检查等，可在 `.env` 改 **`APP_PORT`**。
- 敏感项用 **`.env`** + `config.yaml` 里 `${VAR}`，避免把密钥提交进仓库。

### 与本机其它服务是否冲突？

- **复用 slowwindow MySQL**：只要 **Docker 网络名配对**、库 `onepaper` 已建，一般**无端口冲突**（对方 MySQL 可不对外暴露 3306）。
- **独立 MySQL 模式**：若宿主机 3306 已被占用，在 `.env` 设置 **`MYSQL_PORT=3307`** 等。

### 微信说明

- 需已认证公众号，且接口权限包含素材与草稿/发布。
- `wechat.publish_mode`:
  - `draft`：只写入草稿箱（推荐先人工检查）。
  - `publish`：创建草稿后调用发布接口（异步，可用官方接口轮询状态）。
  - `none`：不上传微信，仍会本地生成 HTML（若未配置 `app_id` 会报错，请用 `none` 或补齐密钥）。
- 正文内图片使用 `material.ImageUpload` 返回的 URL；封面使用 `MediaTypeThumb` 永久素材。
- **至少成功一张配图**（否则无法上传缩略图）。若 arXiv 页与 PDF 均失败，请换论文或手动补图（可后续扩展默认占位图）。

## 成本控制

- 只对 Top 20 做短评分 prompt；仅 Top 5 进入长文生成。
- AI 请求带重试与限速（`pkg/ai`）。

## 扩展点

- 新数据源：实现 `crawler.Source`，在调度中聚合结果。
- OSS：`config.oss` 预留，可在 `image` 或 `publisher` 中接入上传。
- 模型切换：修改 `config.yaml` 中 `score_model` / `article_model`。

## License

MIT（可按需修改）
