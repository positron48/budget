DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_payout_events) OR EXISTS(SELECT 1 FROM investment_payout_event_revisions)
 OR EXISTS(SELECT 1 FROM investment_payout_links) OR EXISTS(SELECT 1 FROM investment_payout_link_revisions) THEN
  RAISE EXCEPTION USING ERRCODE='55000', MESSAGE='forecast evidence exists; refusing destructive downgrade';
 END IF;
END $$;
DROP TABLE investment_payout_link_revisions;
DROP TABLE investment_payout_links;
DROP TABLE investment_payout_event_revisions;
DROP TABLE investment_payout_events;
