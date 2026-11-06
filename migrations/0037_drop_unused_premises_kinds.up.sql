-- В доме нет кладовых и нежилых помещений, лишние виды отвлекают: убираем их из справочника (мягко, код остаётся занятым).
-- Вид, которым пользуются действующие помещения или услуги, не трогаем.
UPDATE premises_kinds k SET deleted_at = now(), updated_at = now()
 WHERE k.code IN ('storage', 'non_residential') AND k.deleted_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM premises p WHERE p.kind = k.code AND p.deleted_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM services s WHERE s.deleted_at IS NULL AND k.code = ANY (s.premises_kinds));
