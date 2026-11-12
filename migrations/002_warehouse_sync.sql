-- Columns for the warehouse cache. Ent Schema.Create adds the same columns on startup.
-- Sample rows (莫斯科一号仓 / 20605650762000 and its siblings) are removed in Go
-- when the process is not in mock mode, so a mock-mode database is left alone.

ALTER TABLE public.shops
    ADD COLUMN IF NOT EXISTS warehouses_synced_at timestamp with time zone,
    ADD COLUMN IF NOT EXISTS warehouses_mock boolean DEFAULT false NOT NULL;
