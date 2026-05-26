# PgBouncer for the Ultraviolet control plane

Transaction-pooled connection multiplexer in front of the control-plane
Postgres. Cuts server-side connection count from N (api+sync replicas) × pool
size down to `default_pool_size = 20`.

## Files

- `pgbouncer.ini` — main config. `pool_mode = transaction`,
  `default_pool_size = 20`, listen `:6432`.
- `userlist.txt` — SCRAM-SHA-256 password hashes. **Placeholders only.**
  Regenerate before any deploy.

## Local (docker-compose)

Add this service to `docker-compose.yml`:

```yaml
  pgbouncer:
    image: edoburu/pgbouncer:1.23.1
    depends_on:
      - postgres
    environment:
      DB_HOST: postgres
      DB_PORT: 5432
      DB_USER: uv
      DB_PASSWORD: uv
      DB_NAME: uv
      POOL_MODE: transaction
      MAX_CLIENT_CONN: "500"
      DEFAULT_POOL_SIZE: "20"
    ports:
      - "6432:6432"
    volumes:
      - ./deploy/pgbouncer/pgbouncer.ini:/etc/pgbouncer/pgbouncer.ini:ro
      - ./deploy/pgbouncer/userlist.txt:/etc/pgbouncer/userlist.txt:ro
```

Then point `DATABASE_URL` at `postgres://uv:uv@pgbouncer:6432/uv?sslmode=disable`.

## Production notes

- Regenerate `userlist.txt` from `pg_shadow` — never ship the placeholders.
- Enable `client_tls_sslmode = require` + `server_tls_sslmode = verify-full`.
- `pool_mode = transaction` rules out session-scoped state: no `SET LOCAL`
  across statements, no `LISTEN/NOTIFY`, no advisory locks held across
  statements. Audit any new sqlc query that relies on these.
