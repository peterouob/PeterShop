-- name: InsertOutBox :execrows

insert into outbox (order_no, payload)
select @order_no::bigint,
       jsonb_build_object(
               'order_no', @order_no::bigint,
               'user_id', @user_id::text,
               'product_id', sa.product_id,
               'activity_id', sa.activity_id,
               'amount', sa.price,
               'created_at_ms', (extract(epoch from now()) * 1000)::bigint)
from seckill_activity sa
where sa.activity_id = @activity_id::bigint;