DO $migration$
BEGIN
 IF EXISTS(SELECT 1 FROM market_data_sources) THEN
  RAISE EXCEPTION 'Refusing to discard market access configuration' USING ERRCODE='55000';
 END IF;
END
$migration$;
DROP TABLE IF EXISTS market_data_sources;
