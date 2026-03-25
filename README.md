# onepaper

论文抓取 → AI 筛选 → 中文成稿 → 配图 → 公众号 HTML → 微信草稿/发布（Go）。

## 功能概览

- **crawler**：`crawler.sources` 可选 arxiv、openalex、pubmed、crossref、semantic_scholar；多源会去重、按时间截断到 `arxiv_max_results`，`lookback_hours` 控制窗口。
- **unpaywall**：配置 `unpaywall.email` 可为 DOI 论文补 OA PDF URL。
- **filter / summarizer / image / publisher / scheduler / repository**：见 `config.example.yaml`（打分 → `filter.top_n` 篇长文；无章节图时用 `wechat.default_thumb`；`database.dsn` 可空不落库）。

## 文章生成流程（单次调度 / `-run-once`）

一次运行产出**一篇**公众号长文（素材来自至多 `filter.top_n` 篇论文），顺序如下：

1. **抓取**：按 `crawler.sources` 与时间窗拉取候选论文，总量上限为 `arxiv_max_results`。
2. **Unpaywall**（配置了 `unpaywall.email` 时）：为带 DOI 的条目尝试补齐开放获取 PDF 链接。
3. **过滤 PDF**：`require_pdf` 为 true 时丢弃没有 `PDFURL` 的条目；若为空且 `fail_fast` 为 true 则直接报错结束本轮（`fail_fast` 为 false 则静默跳过）。
4. **短评打分**：对剩余每篇论文调用 LLM，按标题+摘要打分；配置了 MySQL 时顺带写入 paper 相关记录。若没有任何一篇打分成功则失败退出。
5. **截断**：按分数从高到低排序，取前 `filter.top_n` 篇作为成文输入。
6. **成文**：用上述论文生成**一篇**固定结构中文稿（含全文**核心观点**、每节**简要评论**与正文；**作者**与**原文链接**由程序从论文元数据写入，再渲染为 HTML）。
7. **配图**：按各节关联的论文抽主图（有则插入；默认不拉整本 PDF，失败即跳过该节图）。
8. **微信**：将正文与图片上传素材库，按 `publish_mode` 写草稿或发布（`none` 则跳过微信接口但仍生成 HTML）。
9. **落库**（配置了 `database.dsn` 时）：保存成品文章标题、HTML 与状态。

## 目录结构

```
cmd/server/
config/
internal/{crawler,filter,summarizer,image,publisher,scheduler,repository,model}/
pkg/{ai,logger}/
migrations/
Dockerfile
docker-compose.yml
docker-compose.local-mysql.yml
```

## 本机运行

```bash
mysql -u root -p < migrations/001_schema.sql   # 可选
cp config.example.yaml config.yaml             # 编辑 ai、crawler、wechat 等
export AI_API_KEY=sk-...
go run ./cmd/server -config config.yaml
```

- 定时：`scheduler.enabled` + `cron`（默认每天 9:00 上海）。
- 单次：`go run ./cmd/server -config config.yaml -run-once`
- HTTP：`GET /healthz`；微信回调路径见 `wechat.push_path`。

**LLM**：OpenAI 兼容 Chat，`ai.base_url` 以 `/v1` 结尾。Key 可用 yaml 或环境变量（`AI_API_KEY`、`LLM_API_KEY`、`OPENROUTER_API_KEY` 等，见 `config/config.go`）；`base_url` 也可来自 `AI_BASE_URL`。

## Docker

- **复用已有 MySQL**（如与其它 compose 栈同网）：`docker network ls` 得到网络名 → 写入 `.env` 的 `EXTERNAL_MYSQL_NETWORK`；对 MySQL 容器执行 `migrations/001_schema.sql`；`cp config.docker.example.yaml config.yaml`，dsn 主机指向对方容器名（如 `slowwindow-mysql`）；`cp .env.example .env` 填 LLM、网络、DB 密码等；`docker compose up -d --build`。
- **自带 MySQL**：`docker compose -f docker-compose.local-mysql.yml up -d --build`，dsn 主机用 `mysql`。

镜像用 `golang:1.25-alpine` 与 `GOTOOLCHAIN=local`；海外构建可加 `--build-arg GOPROXY=https://proxy.golang.org,direct`。

## 微信

需公众号接口权限（素材、草稿/发布）。公众平台「服务器配置」URL 与 `push_path` 一致，反代到 `server.addr`。`publish_mode`：`draft` | `publish` | `none`。无章节配图须配置 `default_thumb`（默认可执行目录下 `default.png`）。

## 扩展

新数据源：实现 `crawler.Source` 并写入 `crawler.sources`。`config.oss` 预留。

## License

MIT（可按需修改）
