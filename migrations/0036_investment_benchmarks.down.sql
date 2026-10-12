DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_benchmark_revisions) THEN
  RAISE EXCEPTION 'Confirmed benchmark history must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE investment_benchmark_revisions;
DROP TABLE investment_benchmarks;
