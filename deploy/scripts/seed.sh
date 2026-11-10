#!/usr/bin/env bash

set -euo pipefail

AID=${1:-1}
STOCK=${2:-10}
NS=${K8S_NS:-peter-shop}
PG_LOCAL=${PGPORT:-5433}
REDIS_LOCAL=6380

psql_seckill() { kubectl exec -i -n "$NS" deploy/postgres -- psql -U postgres -d db_seckill -v ON_ERROR_STOP=1 -q "$@"; }
redis() { kubectl exec -n "$NS" deploy/redis -- redis-cli "$@"; }

forward() {
  nc -z 127.0.0.1 "$2" 2>/dev/null && return
  kubectl port-forward -n "$NS" "deploy/$1" "$2:$3" >/dev/null &
  trap 'kill $(jobs -p) 2>/dev/null' EXIT
  for _ in $(seq 20); do nc -z 127.0.0.1 "$2" 2>/dev/null && return; sleep 0.5; done
  echo "port-forward $1 :$2 not ready" >&2; exit 1
}

psql_seckill -v aid="$AID" -v stock="$STOCK" <<'SQL'
INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
VALUES (:aid, 'demo-product', 100, now() - interval '1 hour', now() + interval '1 day', 2, now(), now())
ON CONFLICT (activity_id) DO UPDATE SET start_at = EXCLUDED.start_at, end_at = EXCLUDED.end_at;
INSERT INTO activity_stock (activity_id, total, available, locked, sold)
VALUES (:aid, :stock, :stock, 0, 0)
ON CONFLICT (activity_id) DO NOTHING;
SQL

forward postgres "$PG_LOCAL" 5432
forward redis "$REDIS_LOCAL" 6379

PGHOST=127.0.0.1 PGPORT="$PG_LOCAL" PGUSER=svc_seckill PGPASSWORD="${PGPASSWORD_seckill:-password}" PGDATABASE=db_seckill \
REDIS_ADDR="127.0.0.1:$REDIS_LOCAL" \
  go run ./service/seckill-service/cmd/rebuild --activity "$AID"

echo "activity $AID: db=$(psql_seckill -tAc "SELECT available FROM activity_stock WHERE activity_id = $AID")" \
     "redis=$(redis GET "seckill:{$AID}:stock")"
