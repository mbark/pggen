-- Sums data usage per subscriber over a time window. Inputs use ClickHouse's
-- own parameter syntax, so this file also runs as-is in clickhouse-client.
--
-- sql=FindDataUsageSQL also emits the query text as an exported constant, for
-- a caller that has to put the query inside another statement rather than run
-- it — see query.sql_test.go, which materializes it into a temporary table.
-- name: FindDataUsage :many sql=FindDataUsageSQL
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

-- Two queries share one row struct through output=. The struct is declared
-- once, and chgen checks the two agree on every column before sharing it.
-- name: FindChargesByProvider :many output=ChargeRow
SELECT
    a_num,
    sum(charge) AS charge,
    max(end_date) AS last_end
FROM cdr
WHERE provider = {provider:String}
GROUP BY a_num
ORDER BY a_num;

-- The same shape over a different filter. end_date is Nullable here too, so
-- the shared struct keeps the pointer.
-- name: FindChargesByLabel :many output=ChargeRow
SELECT
    a_num,
    sum(charge) AS charge,
    max(end_date) AS last_end
FROM cdr
WHERE has(labels, {label:String})
GROUP BY a_num
ORDER BY a_num;

-- A parameter named after a ClickHouse setting. Inference sends parameters as
-- settings, so it has to rename them to describe the query; see
-- ch.RenameParams.
-- name: ListCDRsPaged :many
SELECT a_num, units
FROM cdr
WHERE provider = {provider:String}
ORDER BY a_num, start_date
LIMIT {limit:UInt32} OFFSET {offset:UInt32};
