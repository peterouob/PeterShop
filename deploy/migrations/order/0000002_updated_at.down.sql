drop trigger if exists trg_orders_updated_at on orders;
drop trigger if exists trg_tcc_transaction_updated_at on tcc_transaction;
drop trigger if exists trg_tcc_branch_updated_at on tcc_branch;
drop function if exists set_updated_at();
