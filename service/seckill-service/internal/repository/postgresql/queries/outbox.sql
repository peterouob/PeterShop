-- name: DeleteOutBox :execrows
delete from outbox where order_no = @order_no::bigint;
