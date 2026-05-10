# OrbitJob 生产部署

systemd + Go 二进制部署

## 前置条件

- PostgreSQL 17
- golang-migrate v4
- Go 1.26+(构建用，运行时不需要)
- systemd(systemctl, journalctl)
- nginx(可选)

## 部署目录结构

```text
/opt/orbitjob/
├── bin/           # 二进制文件(含 .prev 回滚备份)
├── current/       # 当期二进制
├── etc/           # 各组件 .env 配置文件
├── var/
│   ├── log/       # 运行时日志
│   └── backups/   # pg_dump 备份
└── repo/          # Git 仓库克隆
```

## 首次部署

```bash
# 创建用户
sudo useradd -r -d /opt/orbitjob -m orbitjob
sudo mkdir -p /opt/orbitjob/{bin,current,etc,var/log,var/backups}
sudo chown -R orbitjob:orbitjob /opt/orbitjob

# 克隆仓库并构建
sudo -u orbitjob git clone https://github.com/s3loy/orbitjob.git /opt/orbitjob/repo
cd /opt/orbitjob/repo
for cmp in admin-api scheduler dispatcher worker; do
    CGO_ENABLED=0 go build -o /opt/orbitjob/current/${cmp} ./cmd/${cmp}
done

# 复制 env 模板，填入实际值
for cmp in admin-api scheduler dispatcher worker; do
    cp deploy/env/${cmp}.env.example /opt/orbitjob/etc/${cmp}.env
done

# 数据库设置
sudo -u postgres psql <<SQL
CREATE USER orbitjob WITH PASSWORD '<YOUR_PASSWORD>';
CREATE DATABASE orbitjob OWNER orbitjob;
GRANT ALL PRIVILEGES ON DATABASE orbitjob TO orbitjob;
\c orbitjob
GRANT ALL ON SCHEMA public TO orbitjob;
SQL

# 数据库迁移
export DATABASE_URL="postgres://orbitjob:<YOUR_PASSWORD>@127.0.0.1:5432/orbitjob?sslmode=disable"
migrate -path db/migrations -database "$DATABASE_URL" up

# 安装 systemd 单元
sudo cp deploy/systemd/orbitjob-*.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now orbitjob-admin-api orbitjob-scheduler orbitjob-dispatcher orbitjob-worker
```

## 验证

```bash
systemctl status orbitjob-admin-api orbitjob-scheduler orbitjob-dispatcher orbitjob-worker

curl http://127.0.0.1:8080/healthz     # Admin API
curl http://127.0.0.1:6060/healthz     # Scheduler
curl http://127.0.0.1:6061/healthz     # Dispatcher
curl http://127.0.0.1:6062/healthz     # Worker

curl http://127.0.0.1:6060/readyz      # Scheduler (含 DB ping)
curl http://127.0.0.1:6061/readyz      # Dispatcher
curl http://127.0.0.1:6062/readyz      # Worker

journalctl -u orbitjob-scheduler -f
```

## 升级

```bash
cd /opt/orbitjob/repo
git pull origin main
sudo DATABASE_URL="$DATABASE_URL" bash deploy/scripts/release.sh
```

`release.sh` 执行顺序：迁移 → worker → dispatcher → scheduler → admin-api → 健康检查

单组件升级：

```bash
sudo bash deploy/scripts/upgrade.sh worker
```

## 回滚

`upgrade.sh` 在健康检查失败时自动回滚(`.prev` 备份 → 替换 → 重启)

手动回滚：

```bash
sudo systemctl stop orbitjob-worker
sudo cp /opt/orbitjob/bin/worker.prev /opt/orbitjob/current/worker
sudo systemctl start orbitjob-worker
# 数据库回滚(如需要)
migrate -path db/migrations -database "$DATABASE_URL" down 1
```

## 备份

```bash
# 手动
sudo -u orbitjob bash deploy/scripts/backup.sh

# 定时(crontab，每天 02:00)
# 0 2 * * * /opt/orbitjob/repo/deploy/scripts/backup.sh

# 调整保留天数(默认 30)
RETENTION_DAYS=60 sudo -u orbitjob bash deploy/scripts/backup.sh
```

## Nginx 反向代理(可选)

```bash
sudo cp deploy/nginx/orbitjob.conf /etc/nginx/sites-available/orbitjob
sudo ln -s /etc/nginx/sites-available/orbitjob /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

速率限制：写端点 10 r/s，读端点 60 r/s，`/metrics` 和 `/healthz` 不限。
