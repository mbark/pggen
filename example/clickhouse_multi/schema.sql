-- Two query files read this one table. The point of the example is the
-- package-level machinery — one Querier over several files, a shared row
-- struct declared once, a narrowed genericConn — rather than the type
-- vocabulary, which example/clickhouse_cdr covers.
CREATE TABLE call_record (
    msisdn_a    String,
    record_type Enum8('MOC' = 1, 'SMO' = 2, 'GPRS' = 7),
    provider    LowCardinality(String),
    units       Int64
) ENGINE = MergeTree()
ORDER BY (provider, msisdn_a);
