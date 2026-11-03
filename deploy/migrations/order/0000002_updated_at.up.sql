create function set_updated_at() returns trigger
    language plpgsql as
$$
begin
    new.updated_at := now();
    return new;
end;
$$;

create trigger trg_orders_updated_at before update on orders
    for each row execute function set_updated_at();

create trigger trg_tcc_transaction_updated_at before update on tcc_transaction
    for each row execute function set_updated_at();

create trigger trg_tcc_branch_updated_at before update on tcc_branch
    for each row execute function set_updated_at();
