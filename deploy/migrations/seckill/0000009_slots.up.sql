create table activity_slot (
    activity_id bigint not null references seckill_activity (activity_id),
    slot_no int not null ,
    primary key (activity_id, slot_no),
    disabled_at timestamptz(3),
    constraint ck_slot_no_positive check ( slot_no > 0 )
);

insert into activity_slot (activity_id,slot_no)
select s.activity_id, g
from activity_stock s , generate_series(1, s.total)g;

DO $$
    begin
        if exists(
            select 1 from (select activity_id, count(*) as n
                           from stock_ledger where status <> 3
                                             GROUP BY activity_id ) l
                     left join activity_stock s using (activity_id)
                     where l.n > coalesce(s.total, 0)
        ) then
            raise exception 'active ledger exceeds total: cannot assign slots';
            end if;
    end$$;

ALTER TABLE stock_ledger ADD COLUMN slot_no int;

update stock_ledger l
set slot_no = r.rn
from (
    select order_no,
           row_number() over (partition by activity_id order by order_no) as rn
    from stock_ledger
    where status <> 3
     ) r
where l.order_no = r.order_no;

UPDATE stock_ledger SET slot_no = 1 WHERE slot_no IS NULL;

ALTER TABLE stock_ledger ALTER COLUMN slot_no SET NOT NULL;

alter table stock_ledger
add constraint fk_ledger_slot
foreign key (activity_id, slot_no)
references activity_slot (activity_id, slot_no)
on delete restrict;

create unique index uk_ledger_active_slot
on stock_ledger (activity_id, slot_no)
where status <> 3;

create function ledger_check_slot_enabled() returns trigger as $$
    declare d timestamptz;
    begin
        select disabled_at INTO d
        from activity_slot
        where activity_id = NEW.activity_id
        and slot_no = NEW.slot_no
        for share
        limit 1;

        if not FOUND then
            return NEW;
        end if;

        if d is not null then
            raise exception 'slot % disabled', NEW.slot_no
                USING errcode = '23514', constraint = 'ck_ledger_slot_enabled';
        end if;

        return NEW;
    end
$$ language plpgsql;

create trigger trg_ledger_slot_enabled_ins
    before insert on stock_ledger
    for each row when (new.status <> 3)
execute function ledger_check_slot_enabled();

create trigger trg_ledger_slot_enabled_upd
    before update of slot_no, status on stock_ledger
    for each row when (old.status = 3 and new.status <> 3
    or old.slot_no is distinct from new.slot_no)
execute function ledger_check_slot_enabled();

drop function stock_lock(bigint,bigint,varchar);
drop table activity_stock;