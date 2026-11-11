drop trigger trg_ledger_slot_enabled_upd on stock_ledger;
drop trigger trg_ledger_slot_enabled_ins on stock_ledger;
drop function ledger_check_slot_enabled();

create table activity_stock (
    activity_id bigint primary key
        references seckill_activity (activity_id),
    total       int not null,
    available   int not null,
    locked      int not null,
    sold        int not null,

    constraint ck_stock_non_negative check (available >= 0 and locked >= 0 and sold >= 0),
    constraint ck_stock_i2 check (total = available + locked + sold),
    constraint ck_stock_bigger_zero check ( total > 0 )
) with (fillfactor = 70);

insert into activity_stock (activity_id, total, available, locked, sold)
select s.activity_id,
       s.total,
       s.total - coalesce(l.locked, 0) - coalesce(l.sold, 0),
       coalesce(l.locked, 0),
       coalesce(l.sold, 0)
from (select activity_id, count(*) filter (where disabled_at is null) as total
      from activity_slot
      group by activity_id) s
left join (select activity_id,
                  count(*) filter (where status = 1) as locked,
                  count(*) filter (where status = 2) as sold
           from stock_ledger
           group by activity_id) l using (activity_id);

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

drop index uk_ledger_active_slot;
alter table stock_ledger drop constraint fk_ledger_slot;
alter table stock_ledger drop column slot_no;

drop table activity_slot;
