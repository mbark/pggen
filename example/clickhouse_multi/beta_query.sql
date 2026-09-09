-- A second file in the same package. Its queries reach the Querier interface
-- the leader declares, and it shares the leader's row struct.

-- Sums usage for one subscriber, sharing UsageRow across files.
-- name: SumByMsisdn :many output=UsageRow
SELECT
    msisdn_a,
    any(record_type) AS record_type,
    sum(units)       AS units
FROM call_record
WHERE msisdn_a = {msisdn_a:String}
GROUP BY msisdn_a;
