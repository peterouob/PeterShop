-- name: InsertLedgerWithOutbox :execrows
with ins as (
    insert into stock_ledger (order_no, activity_id, user_id, status, slot_no, created_at, updated_at)
    values (@order_no, @activity_id, @user_id, 1, @slot_no, now(), now())
    on conflict (order_no) do nothing
    returning order_no, activity_id, user_id
)
insert into outbox (order_no, payload)
select ins.order_no,
       jsonb_build_object(
               'order_no', ins.order_no,
               'user_id', ins.user_id,
               'product_id', sa.product_id,
               'activity_id', sa.activity_id,
               'amount', sa.price,
               'created_at_ms', (extract(epoch from now()) * 1000)::bigint)
from ins
join seckill_activity sa on sa.activity_id = ins.activity_id;
