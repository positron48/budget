DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM recurring_payment_templates) OR EXISTS(SELECT 1 FROM recurring_payment_occurrences) OR EXISTS(SELECT 1 FROM recurring_payment_links) THEN
 RAISE EXCEPTION 'Cannot remove recurring payments with user data'; END IF;
END $$;
DROP TABLE recurring_payment_links;
DROP TABLE recurring_payment_occurrences;
DROP TABLE recurring_payment_templates;
DROP INDEX categories_tenant_id_id_payment;
DROP INDEX transactions_tenant_id_id_payment;
