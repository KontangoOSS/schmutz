# Runbook

Step-by-step operational procedures for APPNAME.

## Deploy a new version

??? abstract "Steps"

    1. Merge your PR to `main`
    2. Woodpecker CI triggers automatically
    3. Verify the deployment:
        ```bash
        curl -s https://APPNAME.konoss.org/api/v1/health | jq .
        ```

## Rollback a deployment

!!! danger "Use with caution"
    Rolling back may cause data inconsistencies if migrations ran forward.

??? abstract "Steps"

    1. Find the previous working image tag:
        ```bash
        docker images | grep APPNAME
        ```

    2. Update the compose file on the target LXC:
        ```bash
        ssh root@<lxc-ip> "cd /opt/APPNAME && \
          sed -i 's|image:.*|image: git.konoss.org/APPORG/APPNAME:previous-tag|' docker-compose.yml && \
          docker compose up -d"
        ```

    3. Verify the rollback:
        ```bash
        curl -s https://APPNAME.konoss.org/api/v1/health | jq .version
        ```

## Database operations

### Run a migration

```bash
ssh root@<lxc-ip> "docker exec APPNAME ./migrate up"
```

### Backup the database

```bash
ssh root@<lxc-ip> "docker exec APPNAME-db \
  pg_dump -U APPNAME APPNAME | gzip > /opt/backups/db-$(date +%Y%m%d).sql.gz"
```

### Restore from backup

```bash
ssh root@<lxc-ip> "gunzip -c /opt/backups/db-YYYYMMDD.sql.gz | \
  docker exec -i APPNAME-db psql -U APPNAME APPNAME"
```

## Troubleshooting

### App won't start

```bash
# Check container logs
ssh root@<lxc-ip> "docker compose -f /opt/APPNAME/docker-compose.yml logs --tail 50"

# Check if the port is in use
ssh root@<lxc-ip> "ss -tlnp | grep 8080"

# Check environment variables
ssh root@<lxc-ip> "docker compose -f /opt/APPNAME/docker-compose.yml config"
```

### Database connection refused

```bash
# Check if PostgreSQL is running
ssh root@<lxc-ip> "docker exec APPNAME-db pg_isready"

# Check connection from app container
ssh root@<lxc-ip> "docker exec APPNAME nc -zv APPNAME-db 5432"
```

### CI pipeline failing

| Error | Cause | Fix |
|-------|-------|-----|
| `secret not found` | Missing Woodpecker secret | Add via Woodpecker UI |
| `PHASE_SERVICE_TOKEN` | Phase token expired | Rotate in Phase, update WP secret |
| `exit code 137` | OOM killed | Increase container memory |
| `TLS internal error` | DNS resolution failure | Check DNS records |
