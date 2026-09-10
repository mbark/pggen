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

-- Returns the two columns whose Go types are built out of wrappers. A pointer
-- reports no import of its own and a map reports none either — its key and
-- value can come from different packages — so this file needs "time" from
-- inside a *time.Time and nothing else brings it in.
-- name: FindEndsByMsisdn :many
SELECT
    ended_at,
    tag_sets
FROM call_record
WHERE msisdn_a = {msisdn_a:String}
ORDER BY units;
