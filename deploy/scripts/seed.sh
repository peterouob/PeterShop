#!/usr/bin/env bash

set -euo pipefail

AID=${1:-1}
STOCK=${2:-10}
NS=${K8S_NS:-peter-shop}

psql_seckill() { kubectl exec -i -n "$NS" deploy/postgres -- psql -U postgres -d db_seckill -v ON_ERROR_STOP=1 -q "$@"; }
redis() { kubectl exec -n "$NS" deploy/redis -- redis-cli "$@"; }

psql_seckill -v aid="$AID" -v stock="$STOCK" <<'SQL'
INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
VALUES (:aid, 'demo-product', 100, now() - interval '1 hour', now() + interval '1 day', 2, now(), now())
ON CONFLICT (activity_id) DO NOTHING;
INSERT INTO activity_stock (activity_id, total, available, locked, sold)
VALUES (:aid, :stock, :stock, 0, 0)
ON CONFLICT (activity_id) DO NOTHING;
SQL

now_ms=$(redis TIME | head -1)000
redis HSET "seckill:{$AID}:meta" start_ms $((now_ms - 3600000)) end_ms $((now_ms + 86400000)) >/dev/null
redis SET "seckill:{$AID}:stock" "$STOCK" NX >/dev/null

echo "activity $AID: db=$(psql_seckill -tAc "SELECT available FROM activity_stock WHERE activity_id = $AID")" \
     "redis=$(redis GET "seckill:{$AID}:stock")"
