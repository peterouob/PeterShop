-- name: LoadActivitySnapshot :one
SELECT
    a.start_at,
    a.end_at,
    a.status,
    c.slot_total,
    c.available,
    COALESCE(
        (SELECT array_agg(l.user_id)
         FROM stock_ledger l
         WHERE l.activity_id = a.activity_id),
        '{}'
    )::text[] AS user_ids
FROM seckill_activity a
CROSS JOIN LATERAL (
    SELECT
        count(*)::int as slot_total,
        count(*) FILTER (
            WHERE sl.disabled_at is null
            and l.slot_no is null
        )::int as available
    from activity_slot sl
    left join stock_ledger l
        on l.activity_id = sl.activity_id
        and l.slot_no = sl.slot_no
        and l.status <> 3
    where sl.activity_id = a.activity_id
    ) c
where a.activity_id = $1;