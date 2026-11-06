-- У машиноместа может быть несколько собственников (доли), проживающих по-прежнему не бывает:
-- снимаем «один собственник», правило no_residents остаётся (migration 0035).
UPDATE premises_kinds SET single_owner = false WHERE code = 'parking_space';
