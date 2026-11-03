create function set_updated_at() returns trigger
    language plpgsql as
$$
begin
    new.updated_at := now();
    return new;
end;
$$;

create trigger trg_users_updated_at before update on users
    for each row execute function set_updated_at();
