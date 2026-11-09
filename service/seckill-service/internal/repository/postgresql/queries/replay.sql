-- name: ListReplayEvents :many
select l.order_no,
       l.user_id,
       sa.product_id,
       l.activity_id,
       sa.price                                       as amount,
       (extract(epoch from l.created_at) * 1000)::bigint as created_at_ms
from stock_ledger l
         join seckill_activity sa on sa.activity_id = l.activity_id
where l.activity_id = @activity_id::bigint
  and l.status in (1, 2)
  and l.order_no > @after_order_no::bigint
order by l.order_no
limit @batch_size::int;
