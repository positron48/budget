DO $migration$
BEGIN
 IF EXISTS(SELECT 1 FROM market_data_jobs) THEN
  RAISE EXCEPTION 'Refusing to discard market job history' USING ERRCODE='55000';
 END IF;
END
$migration$;
DROP TABLE IF EXISTS market_data_coverage;
DROP TABLE IF EXISTS market_data_jobs;
