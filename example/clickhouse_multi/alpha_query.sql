-- The lexicographically first source file is the leader: it declares the
-- Querier interface covering every file in the package, the DBQuerier, and the
-- shared output= row struct.

-- Sums usage per subscriber for one provider.
-- name: SumByProvider :many output=UsageRow
SELECT
    msisdn_a,
    any(record_type) AS record_type,
    sum(units)       AS units
FROM call_record
WHERE provider = {provider:String}
GROUP BY msisdn_a
ORDER BY msisdn_a;

-- The same shape over a different filter, so the two share one Go struct.
-- name: SumBusySubscribers :many output=UsageRow
SELECT
    msisdn_a,
    any(record_type) AS record_type,
    sum(units)       AS units
FROM call_record
GROUP BY msisdn_a
HAVING units >= {min_units:Int64}
ORDER BY msisdn_a;
