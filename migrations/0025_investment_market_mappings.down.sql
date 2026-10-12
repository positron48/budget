DO $migration$
BEGIN
 IF EXISTS(SELECT 1 FROM investment_market_mapping_revisions) THEN
  RAISE EXCEPTION 'Refusing to discard confirmed investment mapping history' USING ERRCODE='55000';
 END IF;
END
$migration$;
DROP TABLE IF EXISTS investment_market_mapping_changes;
DROP TABLE IF EXISTS investment_market_mapping_revisions;
DROP TABLE IF EXISTS investment_market_mappings;
