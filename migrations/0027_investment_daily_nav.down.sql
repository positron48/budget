DO $migration$
DECLARE tab text;
BEGIN
 IF EXISTS(SELECT 1 FROM investment_daily_nav_revisions) THEN
  RAISE EXCEPTION 'Refusing to discard NAV projection history' USING ERRCODE='55000';
 END IF;
 FOREACH tab IN ARRAY ARRAY['investment_ledgers','market_provider_observations','investment_market_mappings','market_data_sources','investment_fx_policies','investment_fx_points'] LOOP
  EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I','nav_generation_'||tab,tab);
 END LOOP;
END
$migration$;
DROP FUNCTION IF EXISTS bump_investment_valuation_generation();
DROP TABLE IF EXISTS investment_daily_nav_revisions;
DROP TABLE IF EXISTS investment_daily_nav;
DROP TABLE IF EXISTS investment_nav_series;
DROP TABLE IF EXISTS investment_valuation_generations;
