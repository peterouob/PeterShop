create or replace function stock_lock(p_order_no bigint, p_activity_id bigint, p_user_id varchar)
returns boolean language plpgsql as $$
begin
    insert into
        stock_ledger(order_no, activity_id, user_id, status,created_at, updated_at)
    values (p_order_no, p_activity_id,p_user_id,1,now(),now())
    on conflict (order_no) do nothing;

    if not FOUND then
        return false;
    end if;

    update
        activity_stock
    set
        available = available - 1,
        locked = locked + 1
    where
        activity_id = p_activity_id;

    if not FOUND then
        raise exception 'activity_stock not found: activity_id=%', p_activity_id
        using errcode = 'no_data_found';
    end if;

    return true;
end
$$;