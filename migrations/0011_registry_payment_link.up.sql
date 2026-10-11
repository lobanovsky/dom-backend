-- Реестр — детализация одного платежа из выписки: банк переводит сумму реестра одним платежом и пишет в назначении
-- имя файла реестра. payment_id связывает реестр с этим платежом (см. internal/store/registry_link.go).
ALTER TABLE payment_registries ADD COLUMN payment_id BIGINT REFERENCES incoming_payments (id);
CREATE UNIQUE INDEX payment_registries_payment_id_key ON payment_registries (payment_id) WHERE payment_id IS NOT NULL;

-- Сверка уже загруженного: то же правило, что в registryCandidates (имя файла в назначении, сумма = к перечислению,
-- связь однозначна в обе стороны).
WITH cand AS (
    SELECT r.id AS registry_id, p.id AS payment_id,
           count(*) OVER (PARTITION BY r.id) AS per_registry, count(*) OVER (PARTITION BY p.id) AS per_payment
      FROM payment_registries r
      JOIN incoming_payments p ON p.bank_account_id = r.bank_account_id AND p.registry_id IS NULL AND p.deleted_at IS NULL
       AND p.amount = r.total_transferred AND position(lower(r.file_name) IN lower(coalesce(p.purpose, ''))) > 0
     WHERE r.payment_id IS NULL AND r.deleted_at IS NULL
)
UPDATE payment_registries r SET payment_id = c.payment_id
  FROM cand c WHERE c.registry_id = r.id AND c.per_registry = 1 AND c.per_payment = 1;
