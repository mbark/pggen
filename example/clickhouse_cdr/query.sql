-- Sums data usage per subscriber over a time window. Inputs use ClickHouse's
-- own parameter syntax, so this file also runs as-is in clickhouse-client.
-- name: FindDataUsage :many
SELECT
    a_num                AS msisdn,
    sum(units)           AS data_bytes,
    sum(charge)          AS total_charge,
    max(start_date)      AS last_seen
FROM cdr
WHERE a_num IN {msisdns:Array(String)}
  AND start_date >= {from:DateTime}
  AND start_date < {to:DateTime}
GROUP BY a_num
ORDER BY a_num;

-- Returns every column, so the generated row struct covers the whole type
-- vocabulary.
-- name: FindCDRsByProvider :many
SELECT *
FROM cdr
WHERE provider = {provider:String}
ORDER BY a_num, start_date;

-- A single row.
-- name: FindOldestStart :one
SELECT min(start_date) AS oldest
FROM cdr
WHERE provider = {provider:String};

-- A single column over many rows, which cannot use struct scanning.
-- name: ListProviders :many
SELECT DISTINCT provider
FROM cdr
ORDER BY provider;

-- name: InsertCDR :exec
INSERT INTO cdr (
    a_num, record_type, rat, provider, units, charge, start_date, end_date,
    updated_at, labels, metadata, subscription_id, customer_id, premium
)
VALUES (
    {a_num:String}, {record_type:String}, {rat:Nullable(String)},
    {provider:String}, {units:Int64}, {charge:Decimal(18, 6)},
    {start_date:DateTime}, {end_date:Nullable(DateTime)},
    {updated_at:DateTime64(3)}, {labels:Array(String)},
    {metadata:Map(String, String)}, {subscription_id:UUID},
    {customer_id:Nullable(UUID)}, {premium:Bool}
);
