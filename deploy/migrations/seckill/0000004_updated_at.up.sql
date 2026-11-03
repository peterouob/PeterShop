create function set_updated_at() returns trigger
    language plpgsql as
$$
begin
    new.updated_at := now();
    return new;
end;
$$;

create trigger trg_seckill_activity_updated_at before update on seckill_activity
    for each row execute function set_updated_at();

create trigger trg_stock_ledger_updated_at before update on stock_ledger
    for each row execute function set_updated_at();
