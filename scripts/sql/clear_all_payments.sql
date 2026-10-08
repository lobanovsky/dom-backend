-- Полная очистка платёжных данных перед загрузкой реальных: все входящие и исходящие платежи,
-- все загруженные выписки и реестры (вместе с сохранёнными файлами) и история определения лицевых счетов.
-- Запускается ВРУЧНУЮ, приложением не используется.
--
--   psql "$DATABASE_URL" -f scripts/sql/clear_all_payments.sql
--
-- Что удаляется физически (не «мягко»), это необратимо:
--   * incoming_payments, outgoing_payments  - все платежи (из выписок, реестров и введённые вручную);
--   * bank_statements                       - все загруженные выписки, включая сохранённые файлы;
--   * payment_registries                    - все загруженные реестры Сбера, включая сохранённые файлы
--                                             (платежи реестров удаляются вместе с остальными, а реестр без платежей
--                                             бессмыслен и мешал бы загрузить тот же файл повторно);
--   * assignment_runs, assignment_run_items - история запусков определения лицевых счетов (ссылается на платежи).
-- Физическое удаление освобождает и ключи защиты от дублей (номера операций Сбера, ключи операций выписок):
-- платежи, ранее помеченные удалёнными, при мягком удалении остаются «занятыми» и блокировали бы повторную загрузку.
--
-- НЕ удаляются: банковские счета, категории платежей, ПРАВИЛА определения лицевых счетов, лицевые счета, дома,
-- помещения, собственники, физлица и юрлица.
--
-- Всё выполняется одной транзакцией. Чтобы только посмотреть, что будет удалено, замените COMMIT на ROLLBACK.

BEGIN;

SELECT 'до очистки' AS этап,
       (SELECT count(*) FROM incoming_payments)  AS входящих,
       (SELECT count(*) FROM outgoing_payments)  AS исходящих,
       (SELECT count(*) FROM bank_statements)    AS выписок,
       (SELECT count(*) FROM payment_registries) AS реестров,
       (SELECT count(*) FROM assignment_runs)    AS запусков_определения;

-- Порядок важен из-за внешних ключей: сначала то, что ссылается на платежи, затем платежи, затем файлы и запуски.
DELETE FROM assignment_run_items;
DELETE FROM incoming_payments;
DELETE FROM outgoing_payments;
DELETE FROM assignment_runs;
DELETE FROM bank_statements;
DELETE FROM payment_registries;

SELECT 'после очистки' AS этап,
       (SELECT count(*) FROM incoming_payments)  AS входящих,
       (SELECT count(*) FROM outgoing_payments)  AS исходящих,
       (SELECT count(*) FROM bank_statements)    AS выписок,
       (SELECT count(*) FROM payment_registries) AS реестров,
       (SELECT count(*) FROM assignment_runs)    AS запусков_определения,
       (SELECT count(*) FROM payment_rules WHERE deleted_at IS NULL) AS правил_осталось;

COMMIT;
