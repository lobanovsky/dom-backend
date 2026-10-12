CREATE OR REPLACE FUNCTION payment_categories_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('incoming_payments', 'category_id', OLD.id, 'incoming payments');
    PERFORM assert_no_active('outgoing_payments', 'category_id', OLD.id, 'outgoing payments');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
