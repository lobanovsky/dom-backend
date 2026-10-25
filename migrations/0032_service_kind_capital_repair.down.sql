-- Услуги вида capital_repair переводятся в дополнительные.
UPDATE services SET kind = 'additional' WHERE kind = 'capital_repair';
ALTER TABLE services DROP CONSTRAINT services_kind_check;
ALTER TABLE services ADD CONSTRAINT services_kind_check CHECK (kind IN ('utility', 'maintenance', 'additional'));
