-- Название дома: короткое имя вместо длинного адреса в таблицах; уникально среди действующих домов без учёта регистра.
ALTER TABLE buildings ADD COLUMN name TEXT;
CREATE UNIQUE INDEX buildings_name_uq ON buildings (lower(name)) WHERE name IS NOT NULL AND deleted_at IS NULL;
