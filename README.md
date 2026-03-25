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
docker-compose.mysql.yml
```

## 快速开始（本机 Go）

### 1. 准备 MySQL（可选）

```bash
mysql -u root -p < migrations/001_schema.sql
```

### 2. 配置

```bash
cp config.example.yaml config.yaml
# 编辑 database.dsn、ai.base_url、wechat 等
export OPENAI_API_KEY=sk-...
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

---

## Docker 部署

### 准备

1. 在项目根目录准备 `config.yaml`（可从 `config.example.yaml` 复制并修改）。
2. **推荐**准备 `.env`：复制 `.env.example` 为 `.env`，填写 `OPENAI_API_KEY` 等。Compose 会自动读取项目根目录的 `.env` 用于变量替换，并通过 `docker-compose.yml` 中的 `environment` 传入容器（供 `config.yaml` 里 `${OPENAI_API_KEY}` 等占位符使用）。
3. 容器内配图目录为卷 `onepaper-data`，挂载在 `/app/data`；若使用 Docker，请在 `config.yaml` 里将 `image.dir` 设为 **`/app/data/images`**（可参考 `config.docker.example.yaml`）。

### 仅应用（自带 MySQL 或不用库）

```bash
docker compose build --no-cache
docker compose up -d
```

查看日志：

```bash
docker compose logs -f onepaper
```

停止：

```bash
docker compose down
```

### 与应用一起启动 MySQL

1. 复制 `config.docker.example.yaml` 为 `config.yaml`（或自行把 `database.dsn` 的主机改为 **`mysql`**，密码与 `docker-compose.mysql.yml` 中 `MYSQL_ROOT_PASSWORD` 一致）。
2. 启动：

```bash
docker compose -f docker-compose.yml -f docker-compose.mysql.yml up -d
```

首次启动 MySQL 会执行 `migrations/001_schema.sql` 初始化表（仅在新数据卷上执行一次）。

### 快速更新（拉代码后重建并启动）

在项目根目录执行：

```bash
git pull
docker compose build
docker compose up -d
```

若使用 MySQL 叠加文件：

```bash
git pull
docker compose -f docker-compose.yml -f docker-compose.mysql.yml build
docker compose -f docker-compose.yml -f docker-compose.mysql.yml up -d
```

说明：

- **`build`**：镜像随代码变更更新；依赖不变时也可只 `docker compose up -d --build` 一步完成构建并启动。
- **数据**：`onepaper-data`（应用数据）与 `mysql-data`（若使用 compose MySQL）由卷持久化；`down` 默认不删卷，数据仍在。

### 端口与环境变量

- 默认映射 **`8080:8080`**，可在 `.env` 中设置 `APP_PORT=9000` 等修改宿主机端口。
- `OPENAI_API_KEY`、`WECHAT_APP_ID`、`WECHAT_APP_SECRET` 建议放在 `.env`，避免写进 `config.yaml` 入库。

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
