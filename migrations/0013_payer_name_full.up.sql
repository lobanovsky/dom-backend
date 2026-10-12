-- Раньше из выписки 1С в payer_name попадало только то, что до первого «//» («ПАО СБЕРБАНК»), а ФИО и адрес терялись.
-- Исходная строка документа лежит в raw_line, восстанавливаем полное имя; правим только обрезанные значения,
-- чтобы не затронуть имена, изменённые вручную. Для outgoing_payments raw_line нет — старые строки остаются как есть.
WITH full_names AS (
    SELECT id, btrim(regexp_replace(substring(raw_line FROM '(?:^|; )Плательщик=([^;]*)'), '\s+', ' ', 'g')) AS full_name
      FROM incoming_payments
     WHERE statement_id IS NOT NULL AND raw_line ~ '(?:^|; )Плательщик=[^;]*//'
)
UPDATE incoming_payments p
   SET payer_name = f.full_name, updated_at = now()
  FROM full_names f
 WHERE p.id = f.id AND p.payer_name = split_part(f.full_name, '//', 1) AND f.full_name <> p.payer_name;
