-- Удаление категории не блокируется платежами: у них категория снимается (платёж становится «пустым» и снова
-- доступен правилам). Снимаем и у удалённых платежей, иначе их нельзя будет восстановить (ссылка на удалённую категорию).
CREATE OR REPLACE FUNCTION payment_categories_check_children() RETURNS trigger AS $$
BEGIN
    UPDATE incoming_payments
       SET category_id = NULL, assigned_by = NULL, rule_id = NULL, run_id = NULL, updated_at = now()
     WHERE category_id = OLD.id;
    UPDATE outgoing_payments
       SET category_id = NULL, assigned_by = NULL, rule_id = NULL, run_id = NULL, updated_at = now()
     WHERE category_id = OLD.id;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
