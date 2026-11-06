-- Телефоны физлиц хранятся только цифрами (79636110726); форматирование делает интерфейс.
-- Правила те же, что у model.NormalizePhone: российские номера с кодом 7, значения с буквами («доб.») не трогаем, дубли убираем.
CREATE FUNCTION normalize_phone(s text) RETURNS text AS $$
DECLARE
    t text := btrim(s);
    d text;
BEGIN
    IF t ~ '[^0-9[:space:]()+.-]' THEN RETURN t; END IF;
    d := regexp_replace(t, '\D', '', 'g');
    IF length(d) = 11 AND left(d, 1) = '8' AND left(t, 1) <> '+' THEN RETURN '7' || substr(d, 2); END IF;
    IF length(d) = 10 AND left(t, 1) <> '+' THEN RETURN '7' || d; END IF;
    RETURN d;
END
$$ LANGUAGE plpgsql IMMUTABLE;

UPDATE persons SET phones = ARRAY(
        SELECT n FROM (SELECT normalize_phone(x) AS n, min(o) AS o FROM unnest(phones) WITH ORDINALITY u(x, o) GROUP BY 1) t
        WHERE n <> '' ORDER BY o)
 WHERE EXISTS (SELECT 1 FROM unnest(phones) x WHERE normalize_phone(x) <> x);

DROP FUNCTION normalize_phone(text);
