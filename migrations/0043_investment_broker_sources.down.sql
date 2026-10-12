DO $guard$
BEGIN
 IF EXISTS(SELECT 1 FROM investment_broker_snapshots) THEN
  RAISE EXCEPTION 'Cannot remove stored broker sources' USING ERRCODE='55000';
 END IF;
END
$guard$;
DROP TABLE investment_broker_snapshot_rows;
DROP TABLE investment_broker_observations;
DROP TABLE investment_broker_snapshots;
