-- Очистка только реестров: все загруженные реестры (с самими файлами) и входящие платежи, созданные из них.
-- Запускается ВРУЧНУЮ, приложением не используется. Цель: загрузить реестры заново, не трогая выписки.
--
--   psql "$DATABASE_URL" -f scripts/sql/clear_registries.sql
--
-- Что удаляется физически (не «мягко»), это необратимо:
--   * payment_registries      - все загруженные реестры (Сбер, ВТБ), включая сохранённые файлы и удалённые «мягко»;
--   * incoming_payments       - только платежи, у которых есть реестр (registry_id IS NOT NULL), в том числе удалённые «мягко»:
--                               при мягком удалении их номера операций остаются «занятыми» и блокировали бы повторную загрузку;
--   * assignment_run_items    - записи истории определения лицевых счетов по этим платежам.
-- Исходящих платежей у реестров нет (реестры бывают только у входящих), поэтому исходящие не затрагиваются.
--
-- НЕ удаляются: платежи из выписок (1С, Sber API) и сами выписки, введённые вручную платежи, запуски определения
-- (assignment_runs), правила, банковские счета, категории, лицевые счета, дома, помещения, собственники.
-- Платёж выписки, которому реестр был детализацией, остаётся; связь с реестром пропадает вместе с реестром,
-- а при следующей загрузке реестра она восстановится автоматически.
-- Для полной очистки платежей, выписок и реестров вместе используйте clear_all_payments.sql.
--
-- Всё выполняется одной транзакцией. Чтобы только посмотреть, что будет удалено, замените COMMIT на ROLLBACK.

BEGIN;

SELECT 'до очистки' AS этап,
       (SELECT count(*) FROM payment_registries)                              AS реестров,
       (SELECT count(*) FROM incoming_payments WHERE registry_id IS NOT NULL) AS платежей_реестров,
       (SELECT count(*) FROM incoming_payments WHERE registry_id IS NULL)     AS остальных_входящих,
       (SELECT count(*) FROM outgoing_payments)                               AS исходящих;

-- Порядок важен из-за внешних ключей: история определения -> платежи реестров -> реестры.
DELETE FROM assignment_run_items
 WHERE payment_id IN (SELECT id FROM incoming_payments WHERE registry_id IS NOT NULL);
DELETE FROM incoming_payments WHERE registry_id IS NOT NULL;
DELETE FROM payment_registries;

SELECT 'после очистки' AS этап,
       (SELECT count(*) FROM payment_registries)                              AS реестров,
       (SELECT count(*) FROM incoming_payments WHERE registry_id IS NOT NULL) AS платежей_реестров,
       (SELECT count(*) FROM incoming_payments WHERE registry_id IS NULL)     AS остальных_входящих,
       (SELECT count(*) FROM outgoing_payments)                               AS исходящих;

COMMIT;
