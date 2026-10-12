DO $migration$
BEGIN
 IF EXISTS(SELECT 1 FROM market_provider_instruments) THEN
  RAISE EXCEPTION 'Refusing to discard market instrument history' USING ERRCODE='55000';
 END IF;
END
$migration$;
DROP TABLE IF EXISTS market_provider_instrument_revisions;
DROP TABLE IF EXISTS market_provider_instruments;
