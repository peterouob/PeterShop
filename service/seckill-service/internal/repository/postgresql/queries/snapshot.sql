-- name: LoadActivitySnapshot :one
SELECT
    a.start_at,
    a.end_at,
    a.status,
    s.available,
    COALESCE(
        (SELECT array_agg(l.user_id)
         FROM stock_ledger l
         WHERE l.activity_id = a.activity_id),
        '{}'
    )::text[] AS user_ids
FROM seckill_activity a
LEFT JOIN activity_stock s ON s.activity_id = a.activity_id
WHERE a.activity_id = $1;