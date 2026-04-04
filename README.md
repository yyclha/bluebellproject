# Bluebell

Bluebell 是一个基于 Go + Gin 的社区项目，包含用户登录注册、帖子列表、帖子详情、点赞、评论，以及基于向量检索的 RAG 搜索能力。

当前仓库已经接入：
- MySQL 持久化帖子、用户、评论等业务数据
- Redis 存储投票等缓存数据
- Milvus 存储帖子分片后的向量数据
- DashScope 兼容接口生成 Embedding

## 功能概览

- 用户注册、登录、JWT 鉴权
- 社区列表、帖子发布、帖子详情
- 点赞投票
- 帖子评论
- RAG 检索
- RAG 重建索引 `reindex`

## 技术栈

- Go
- Gin
- MySQL
- Redis
- Milvus
- Swagger
- Docker Compose

## 项目结构

```text
bluebell/
├─ controller/    HTTP 入口
├─ logic/         业务逻辑
├─ dao/           MySQL / Redis / Milvus 数据访问
├─ models/        数据结构定义
├─ router/        路由注册
├─ setting/       配置加载
├─ templates/     页面模板
├─ static/        前端静态资源
├─ conf/          配置文件
└─ main.go        程序入口
```

## 本地环境要求

- Go 1.23.x
- MySQL 8.x
- Redis 5.x 或更高
- Milvus 2.4.x
- Docker / Docker Compose

## 配置说明

程序默认读取：

```bash
./conf/config.yaml
```

当前仓库为了避免泄露密钥，没有提交 `conf/config.yaml`。你可以基于 `conf/dev.yml` 自行新建一份：

```bash
cp conf/dev.yml conf/config.yaml
```

Windows PowerShell:

```powershell
Copy-Item .\conf\dev.yml .\conf\config.yaml
```

需要重点修改这些配置项：

```yaml
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  password: "你的密码"
  dbname: "bluebell"

redis:
  host: 127.0.0.1
  port: 6379
  password: ""
  db: 6

milvus:
  enabled: true
  address: "127.0.0.1:19530"
  collection: "post_rag_chunk_1024"
  dimension: 1024
  chunk_size: 300
  chunk_overlap: 60
  top_k: 5
  metric_type: "COSINE"
  index_type: "HNSW"
  hnsw_m: 16
  hnsw_ef_construction: 200
  search_ef: 64

embedding:
  enabled: true
  base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
  api_key: "你的 DashScope Key"
  model: "text-embedding-v4"
  timeout_seconds: 30
```

## 数据库初始化

先创建数据库：

```sql
CREATE DATABASE IF NOT EXISTS bluebell;
```

项目启动时会自动初始化评论表，但帖子、用户、社区等基础表你需要提前导入。

可用的 SQL 文件：

- `bluebell_user.sql`
- `bluebell_community.sql`
- `bluebell_post.sql`

导入示例：

```bash
mysql -uroot -p bluebell < bluebell_user.sql
mysql -uroot -p bluebell < bluebell_community.sql
mysql -uroot -p bluebell < bluebell_post.sql
```

Windows PowerShell:

```powershell
Get-Content .\bluebell_user.sql | mysql -uroot -p bluebell
Get-Content .\bluebell_community.sql | mysql -uroot -p bluebell
Get-Content .\bluebell_post.sql | mysql -uroot -p bluebell
```

## 使用 Docker Compose 启动依赖

仓库内提供了 `docker-compose.yml`，包含：

- MySQL
- Redis
- etcd
- MinIO
- Milvus
- bluebell_app

启动：

```bash
docker compose up -d
```

查看状态：

```bash
docker compose ps
```

查看日志：

```bash
docker compose logs -f
```

停止：

```bash
docker compose down
```

如果只想启动依赖，不启动应用，可以按服务名单独起：

```bash
docker compose up -d mysql8019 redis507 etcd minio milvus-standalone
```

## 本地启动项目

安装依赖并启动：

```bash
go run ./main.go ./conf/config.yaml
```

或使用 Makefile：

```bash
make run
```

构建：

```bash
make build
```

代码检查：

```bash
make gotool
```

## 访问地址

- 首页: `http://127.0.0.1:8084/`
- 健康检查: `http://127.0.0.1:8084/ping`
- Swagger: `http://127.0.0.1:8084/swagger/index.html`

如果通过 Docker Compose 启动 `bluebell_app`，映射端口是：

- `http://127.0.0.1:8888/`

## 常用接口示例

注册：

```bash
curl -X POST http://127.0.0.1:8084/api/v1/signup \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"test1\",\"password\":\"123456\",\"re_password\":\"123456\"}"
```

登录：

```bash
curl -X POST http://127.0.0.1:8084/api/v1/login \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"test1\",\"password\":\"123456\"}"
```

获取帖子列表：

```bash
curl "http://127.0.0.1:8084/api/v1/posts?page=1&size=10&order=score"
```

获取帖子详情：

```bash
curl "http://127.0.0.1:8084/api/v1/post/1"
```

获取评论列表：

```bash
curl "http://127.0.0.1:8084/api/v1/post/1/comments"
```

发表评论：

```bash
curl -X POST http://127.0.0.1:8084/api/v1/comment \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d "{\"post_id\":\"1\",\"content\":\"这是一条评论\"}"
```

## RAG 说明

### 分片切分规则

帖子进入 RAG 时，会先把标题和正文拼接成一段文本，再进行分片：

```text
title + "\n" + content
```

默认切分参数：

- `chunk_size = 300`
- `chunk_overlap = 60`

切分规则：

- 先做轻量清洗，去掉多余空行和首尾空白
- 按字符窗口滑动切片
- 相邻 chunk 保留 overlap 重叠
- 尽量优先在换行、标点、空格等自然边界截断
- 每个 chunk 单独生成 embedding 并写入 Milvus

对应代码位置：

- [logic/rag_chunk.go](E:\BaiduNetdiskDownload\bluebell\logic\rag_chunk.go)
- [logic/rag.go](E:\BaiduNetdiskDownload\bluebell\logic\rag.go)
- [dao/milvus/milvus.go](E:\BaiduNetdiskDownload\bluebell\dao\milvus\milvus.go)

### RAG 查询接口

```bash
curl "http://127.0.0.1:8084/api/v1/rag/search?query=gin%E6%A1%86%E6%9E%B6&top_k=5"
```

### RAG 重建索引

接口：

```bash
curl -X POST http://127.0.0.1:8084/api/v1/rag/reindex \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d "{\"limit\":1000}"
```

如果你已经改过 Milvus 的 schema，或者之前 collection 不兼容 chunk 模式，建议直接换一个新的 collection 名称，例如：

```yaml
milvus:
  collection: "post_rag_chunk_1024"
```

然后重启服务，再执行 `reindex`。

### 浏览器里直接执行 reindex

如果你在浏览器控制台里调接口，要注意：

- 必须带 JWT token
- 当前接口需要登录鉴权
- `localhost` 和 `127.0.0.1` 会被浏览器当成不同源
- 如果页面来自 `http://localhost:8084`，请求也尽量发到 `http://localhost:8084`

示例：

```javascript
fetch("http://localhost:8084/api/v1/rag/reindex", {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    "Authorization": "Bearer " + localStorage.getItem("token")
  },
  body: JSON.stringify({ limit: 1000 })
}).then(r => r.json()).then(console.log)
```

如果你的 token 不在 `localStorage`，就把实际 token 直接替换进去。

## 常用开发命令

格式化与静态检查：

```bash
go fmt ./...
go vet ./...
```

运行测试：

```bash
go test ./...
```

只编译不运行：

```bash
go build ./...
```

查看 Git 状态：

```bash
git status
```

提交代码：

```bash
git add .
git commit -m "feat: update project"
git push origin master
```

## 常见问题

### 1. RAG reindex 时报 CORS

如果你在浏览器里请求：

```text
http://127.0.0.1:8084/api/v1/rag/reindex
```

但页面本身来自：

```text
http://localhost:8084
```

浏览器会把它当成跨域请求。最简单的处理方式不是立刻改后端，而是统一使用同一个 host。

优先使用：

```text
http://localhost:8084
```

或者统一改成：

```text
http://127.0.0.1:8084
```

### 2. Milvus collection schema 不兼容

如果以前的 collection 不是按 chunk 结构建的，会报 schema 不匹配。解决方式：

- 新建一个新的 collection 名称
- 修改 `conf/config.yaml`
- 重启服务
- 执行 `reindex`

### 3. 启动时报 embedding 初始化失败

通常是以下原因：

- `embedding.api_key` 没填
- `base_url` 不可达
- `model` 名称不正确

## 说明

- 仓库默认未提交 `conf/config.yaml`，避免敏感配置泄露
- 评论表会在启动时自动初始化
- RAG 功能依赖 Embedding 和 Milvus，任一不可用时不会正常建立向量索引
