# 生产部署方案(nginx 静态托管前端 + docker 后端)

目标形态:

```
                       ┌────────────────────────────────────────────┐
  浏览器               │  服务器 (Ubuntu/Debian 等)                   │
  https://your-domain  │                                            │
     │                 │  ┌──────────────────────────────────────┐  │
     ├──443──►  nginx  │  │ 前端静态 /opt/sale-sipeed/dist        │  │
     │                 │  │ (Vite build 产物,SPA)                 │  │
     │                 │  └───────────────┬──────────────────────┘  │
     │                 │                  │ /api/ 原样转发           │
     │                 │                  ▼                        │
     │                 │  ┌──────────────────────────────────────┐  │
     │                 │  │ backend 容器 (127.0.0.1:8080)          │  │
     │                 │  └───────────────┬──────────────────────┘  │
     │                 │                  │ DATABASE_URL            │
     │                 │                  ▼                        │
     │                 │  ┌──────────────────────────────────────┐  │
     │                 │  │ PostgreSQL (服务器本机或托管)           │  │
     │                 │  └──────────────────────────────────────┘  │
     └── 后端由 nginx 反代 ── Stripe webhook 也打到 8080 (经 443)     │
       Stripe 回调 ─────────  https://your-domain/api/webhooks/stripe│
└────────────────────────────────────────────────────────────┘
```

- 前端:`frontend/` 构建出 `dist/`,由宿主机 nginx 静态托管。
- 后端:构建进 `backend` docker 镜像,容器仅监听 `127.0.0.1:8080`(不直接暴露公网)。
- nginx 把 `/api/` **原样**(保留前缀)反代到 `127.0.0.1:8080`;`/healthz` 不反代(运维直连 8080)。
- 数据库:复用**服务器本机 PostgreSQL**(方案 A,推荐)或由 compose 自带(PG 容器,方案 B)。

---

## 0. 前置条件

| 项 | 要求 |
| --- | --- |
| 服务器 | Ubuntu 22.04+/Debian 12+ 或同类,root 或 sudo |
| Docker + Compose | Docker Engine 24+ / Compose v2(`docker compose version`) |
| 域名 | 已解析 A 记录指向服务器公网 IP |
| HTTPS 证书 | 用 certbot 申请(下面 `step 3`) |
| Node.js | 仅构建前端用(服务器可不装,见"在本地构建 dist"节) |
| Go | 不需要——后端由 Docker 多阶段构建(内置 golang) |

生产 Stripe 密钥:`sk_live_...` 与 `whsec_...`(见 `step 3` 的 Stripe 配置)。

---

## 1. 克隆代码

```bash
mkdir -p /opt && cd /opt
git clone https://<your-repo>/sale-sipeed.git
cd sale-sipeed
git checkout <release-tag或master>
```

> 后续升级就 `git pull` 或 `git fetch + checkout 新 tag`,再按 `DEPLOY.md` 的"升级"节重建。

---

## 2. 准备服务器 PostgreSQL(方案 A,推荐)

> 方案 B(PG 容器)见文末"附:B,数据库也跑 docker"。

### 2.1 安装 PG

```bash
sudo apt update
sudo apt install -y postgresql postgresql-contrib   # 装完默认监听 localhost:5432
```

### 2.2 建库建用户(按需改密码)

```bash
sudo -u postgres psql <<'SQL'
CREATE USER sale_sipeed WITH PASSWORD '换一个强密码';
CREATE DATABASE sale_sipeed OWNER sale_sipeed;
SQL
```

### 2.3 确保监听本地

```bash
# /etc/postgresql/*/main/postgresql.conf → listen_addresses = 'localhost'
sudo systemctl enable --now postgresql
```

数据库连接串(供后端 `.env` 用):

```
postgres://sale_sipeed:换一个强密码@127.0.0.1:5432/sale_sipeed?sslmode=disable
```

> 后端容器访问宿主机 PG,用 `127.0.0.1` 而非 `localhost`(容器内 localhost 指容器自己)。上面的连接串已用 `127.0.0.1`。

---

## 3. 配置密钥 `.env`

仓库根放一个 `.env`(compose 自动读取;不要提交 git):

```bash
cd /opt/sale-sipeed
cat > .env <<'EOF'
# ---- 数据库(方案 A:服务器本机 PG)----
DATABASE_URL=postgres://sale_sipeed:换一个强密码@127.0.0.1:5432/sale_sipeed?sslmode=disable

# ---- Stripe 生产 ----
STRIPE_API_KEY=sk_live_...
# webhook 签名密钥:在 Stripe 后台创建 endpoint 后 Reveal secret 得到
STRIPE_WEBHOOK_SECRET=whsec_...

# 站点公网地址(必须 https,拼 success_url / cancel_url)
PUBLIC_BASE_URL=https://your-domain.example.com

# 后端启动时自动跑迁移(首次 true;数据稳定后可改 false,由发布流程控制)
MIGRATIONS_AUTO=true

# 若方案 B(PG 容器),这行会覆盖 DATABASE_URL 指向容器;方案 A 无需设置
# POSTGRES_PASSWORD=xxx
EOF
chmod 600 .env
```

---

## 4. 构建后端镜像并启动容器

```bash
cd /opt/sale-sipeed
docker compose up -d --build
```

- 构建:多阶段(静态编译)→ 运行镜像(alpine,非 root,迁移 SQL 内嵌)。
- 启动:`backend` 容器监听 `127.0.0.1:8080`,仅容器网络/宿主机可见。
- 首启自动执行数据库迁移(`MIGRATIONS_AUTO=true`),并插入种子商品(`make`/迁移内的种子)。

验证:

```bash
curl -s http://127.0.0.1:8080/healthz
# → {"status":"ok"}

curl -s http://127.0.0.1:8080/api/products | jq   # 商品列表
```

---

## 5. 构建前端 dist

两种方式二选一:

### 5A. 服务器上有 Node(推荐 CI 产物上传)

```bash
cd /opt/sale-sipeed/frontend
npm ci
npm run build        # 产出 dist/
sudo mkdir -p /opt/sale-sipeed/dist && sudo chown -R $USER /opt/sale-sipeed/dist
cp -r dist/* /opt/sale-sipeed/dist/
```

### 5B. 本地/CI 构建后上传(服务器免装 Node)

```bash
# 本地
cd frontend && npm ci && npm run build
scp -r dist/* user@server:/opt/sale-sipeed/dist/
```

> 前端构建产物只含静态文件 + 相对 `/api` 调用,不依赖部署域名;构建一次可复用。

---

## 6. 配置 nginx + HTTPS

### 6.1 安装 nginx

```bash
sudo apt install -y nginx
```

### 6.2 放置站点配置(替换 server_name/证书路径)

仓库自带 `nginx/sale-sipeed.conf`,按域名改好后放进去:

```bash
# 把 sale-sipeed.example.com 换成你的域名(三处)
sed 's/sale-sipeed\.example\.com/your-domain.example.com/g' \
  nginx/sale-sipeed.conf | sudo tee /etc/nginx/sites-available/sale-sipeed

sudo ln -s /etc/nginx/sites-available/sale-sipeed /etc/nginx/sites-enabled/   # 若已有 default,先确认不冲突
sudo nginx -t && sudo systemctl reload nginx
```

要点(文件里已写好):
- `root /opt/sale-sipeed/dist` + `try_files ... /index.html`(SPA 回退);
- `/assets/` 一年强缓存(带哈希指纹);
- `/api/` → `proxy_pass http://127.0.0.1:8080;`(**保留 /api 前缀**)。

### 6.3 用 certbot 签证书(80 端口先跑着)

```bash
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d your-domain.example.com
# 自动改写 443 的 ssl_certificate 并配置 80→443 跳转
```

> 证书续期由 certbot 的 timer 自动处理。若用本机 nginx 已有 `http2`,certbot 可能保留;不用手动加。

### 6.4 验证全站

```bash
curl -s https://your-domain.example.com/            # 前端 index.html
curl -s https://your-domain.example.com/api/products | jq
```

---

## 7. Stripe 生产配置(webhook)

生产**不用** `stripe listen`——用真实投递。

1. [Stripe Dashboard → Developers → Webhooks → Add endpoint](https://dashboard.stripe.com/webhooks)
2. URL 填:`https://your-domain.example.com/api/webhooks/stripe`
3. 勾选事件(4 类):
   - `checkout.session.completed`
   - `checkout.session.async_payment_succeeded`
   - `checkout.session.async_payment_failed`
   - `checkout.session.expired`
4. **Reveal secret**,把 `whsec_...` 填进 `/opt/sale-sipeed/.env` 的 `STRIPE_WEBHOOK_SECRET`,然后 `docker compose up -d --build backend`(容器重启加载)。
5. 测试:沙箱下用测试卡 `4242 4242 4242 4242` 走一遍下单,确认:
   - 订单 `PENDING → PAID`(webhook 推进);
   - 数据库 `orders.stripe_payment_intent_id` 有 `pi_...`。

> 生产用 `sk_live_...` 下单会真实扣款;上线前务必先沙箱全流程验收。
> 退款端点在当前示例中未加鉴权(见 README 说明),上线前必须为 `POST /api/orders/{id}/refund` 增加网关 ACL / JWT 鉴权,否则任何人可退款他人订单。

---

## 8. 数据库迁移说明

- 迁移 SQL 内嵌在后端二进制(`backend/internal/store/migrations/`),启动时按
  `MIGRATIONS_AUTO` 自动执行(用 golang-migrate 记录版本)。
- 首次启动自动建表 + 种子商品;后续升级带新迁移也自动跑。
- 生产若想"发布时跑迁移、运行时不自动":把 `MIGRATIONS_AUTO` 设 `false`,
  由 CI 在 `docker compose build` 前先 `docker compose run --rm backend migrate up`
  (暂未封装该子命令,先用 `true` 自动即可)。

---

## 9. 运维

### 日志

```bash
docker compose logs -f backend        # 后端(slog 结构化 JSON)
tail -f /var/log/nginx/access.log     # nginx 访问日志
sudo journalctl -u nginx -f
```

### 健康检查/状态

```bash
curl -s http://127.0.0.1:8080/healthz            # 后端存活
curl -s https://your-domain/api/products | jq    # 端到端
docker ps                                        # 容器状态
```

### 备份(数据库)

```bash
# 每日 cron:pg_dump 到文件并保留 N 天
0 3 * * *  pg_dump postgres://sale_sipeed:***@127.0.0.1:5432/sale_sipeed \
           | gzip > /backup/sale_sipeed-$(date +\%F).sql.gz \
           && find /backup -name 'sale_sipeed-*' -mtime +14 -delete
```

### 恢复

```bash
gunzip < /backup/sale_sipeed-YYYY-MM-DD.sql.gz | \
  psql postgres://sale_sipeed:***@127.0.0.1:5432/sale_sipeed
```

### 升级发布

```bash
cd /opt/sale-sipeed
git pull                       # 或 git fetch && git checkout <new-tag>
# 若前端有变:
cd frontend && npm ci && npm run build && cp -r dist/* /opt/sale-sipeed/dist/
# 后端 + 迁移:
cd .. && docker compose up -d --build backend
# 验证:
curl -s http://127.0.0.1:8080/healthz && curl -s https://your-domain/api/products | jq
```

---

## 附:B,数据库也跑 docker(不想用服务器 PG)

```bash
# .env 里设 POSTGRES_PASSWORD(容器内 DB 密码),其余照抄
cd /opt/sale-sipeed
cat > .env <<'EOF'
POSTGRES_PASSWORD=容器内DB密码
DATABASE_URL=postgres://sale_sipeed:容器内DB密码@postgres:5432/sale_sipeed?sslmode=disable
STRIPE_API_KEY=sk_live_...
STRIPE_WEBHOOK_SECRET=whsec_...
PUBLIC_BASE_URL=https://your-domain.example.com
MIGRATIONS_AUTO=true
EOF
docker compose --profile pg up -d --build
```

> 此时 PG 数据在 named volume `sale_sipeed_pgdata`;备份用 `docker exec <pg容器> pg_dump`。

---

## 附:C,只用一台服务器的快速核对清单

```bash
# 1. 容器
curl -s http://127.0.0.1:8080/healthz
# 2. 前端
curl -sI https://your-domain/ | head -1
# 3. API 端到端
curl -s https://your-domain/api/products | jq '. | length'
# 4. 下单(沙箱 key 下验证到 PAID)
curl -s -X POST https://your-domain/api/orders -H 'Content-Type: application/json' \
  -H "X-Request-Id: $(uuidgen | tr 'A-Z' 'a-z')" \
  -d '{"product_id":"01a181d1-d53a-7428-811c-e53c0548c643","quantity":1}' | jq
# 5. webhook 触发成功后在 PG 看状态
sudo -u postgres psql -d sale_sipeed -c "SELECT id,status,stripe_payment_intent_id FROM orders ORDER BY created_at DESC LIMIT 3;"
```