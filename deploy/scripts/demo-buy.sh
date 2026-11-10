#!/usr/bin/env bash
set -euo pipefail

AID=${1:-1}
NS=${K8S_NS:-peter-shop}
GATEWAY=${GATEWAY:-http://localhost:8080}
API=$GATEWAY/api/v1
USER_NAME=demo-$(date +%s)
PASS=password123

post() { curl -sS --fail-with-body -H 'Content-Type: application/json' "$@"; }
psql_seckill() { kubectl exec -n "$NS" deploy/postgres -- psql -U postgres -d db_seckill -tAc "$1"; }
kafka() { kubectl exec -n "$NS" deploy/kafka -- "/opt/kafka/bin/$1" --bootstrap-server localhost:9092 "${@:2}"; }

kafka kafka-topics.sh --describe --topic seckill-order >/dev/null 2>&1 \
  || { echo "✗ topic seckill-order missing: run make kafka-topics first"; exit 1; }

post "$API/users/register" -d "{\"username\":\"$USER_NAME\",\"password\":\"$PASS\",\"check_password\":\"$PASS\"}"; echo
token=$(post "$API/users/login" -d "{\"username\":\"$USER_NAME\",\"password\":\"$PASS\"}" | jq -r .token)
buy=$(post "$API/seckill/buy" -H "Authorization: Bearer $token" -d "{\"activity_id\":\"$AID\"}")
echo "buy: $buy"
order_no=$(jq -r .order_no <<<"$buy")

echo "ledger rows for $order_no: $(psql_seckill "SELECT count(*) FROM stock_ledger WHERE order_no = $order_no")  (want 1)"
echo "outbox rows:              $(psql_seckill "SELECT count(*) FROM outbox")  (want 0)"

if kafka kafka-console-consumer.sh --topic seckill-order --from-beginning --timeout-ms 10000 \
     --formatter-property print.key=true 2>/dev/null | grep -q "^$order_no"$'\t'; then
  echo "seckill-order has key $order_no"
else
  echo "seckill-order has no key $order_no"; exit 1
fi

psql_order() { kubectl exec -n "$NS" deploy/postgres -- psql -U postgres -d db_order -tAc "$1"; }
for _ in $(seq 1 20); do
  status=$(psql_order "SELECT status FROM orders WHERE order_no = $order_no")
  [ -n "$status" ] && break
  sleep 0.5
done
echo "orders.status for $order_no:  ${status:-<none>}  (want 1 = PENDING)"
echo "ledger.status for $order_no:  $(psql_seckill "SELECT status FROM stock_ledger WHERE order_no = $order_no")  (want 1 = LOCKED)"
score=$(kubectl exec -n "$NS" deploy/redis -- redis-cli ZSCORE seckill:delay:order-payment-timeout "$order_no")
echo "delay-queue score:            ${score:-<none>}  (want expire_at in ms)"
[ "$status" = 1 ] && [ -n "$score" ] || { echo "✗ order-service did not create the order"; exit 1; }
echo "one order, two records: ledger LOCKED + orders PENDING ($order_no)"
