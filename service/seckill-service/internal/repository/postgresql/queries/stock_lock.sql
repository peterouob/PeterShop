-- name: StockLock :one
SELECT stock_lock(
               @order_no::bigint,
               @activity_id::bigint,
               @user_id::varchar
       )::boolean AS locked;