DO $migration$
BEGIN
 IF EXISTS(SELECT 1 FROM market_provider_observations) THEN
  RAISE EXCEPTION 'Refusing to discard market observation history' USING ERRCODE='55000';
 END IF;
END
$migration$;
DROP TABLE IF EXISTS market_data_changes;
DROP TABLE IF EXISTS market_provider_observation_revisions;
DROP TABLE IF EXISTS market_provider_observations;
