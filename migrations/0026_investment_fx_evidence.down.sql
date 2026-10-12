DO $migration$
BEGIN
 IF EXISTS(SELECT 1 FROM investment_fx_policy_revisions) THEN
  RAISE EXCEPTION 'Refusing to discard investment FX evidence' USING ERRCODE='55000';
 END IF;
END
$migration$;
DROP TABLE IF EXISTS investment_fx_changes;
DROP TABLE IF EXISTS investment_fx_point_revisions;
DROP TABLE IF EXISTS investment_fx_points;
DROP TABLE IF EXISTS investment_fx_policy_revisions;
DROP TABLE IF EXISTS investment_fx_policies;
