create function set_updated_at() returns trigger
    language plpgsql as
$$
begin
    new.updated_at := now();
    return new;
end;
$$;

create trigger trg_payment_freeze_updated_at before update on payment_freeze
    for each row execute function set_updated_at();
