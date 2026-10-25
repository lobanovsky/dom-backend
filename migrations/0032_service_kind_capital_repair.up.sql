-- Новый вид услуги: взнос на капитальный ремонт (отдельной строкой, на отдельный счёт).
ALTER TABLE services DROP CONSTRAINT services_kind_check;
ALTER TABLE services ADD CONSTRAINT services_kind_check CHECK (kind IN ('utility', 'maintenance', 'additional', 'capital_repair'));
