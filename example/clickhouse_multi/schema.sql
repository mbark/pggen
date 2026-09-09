-- Two query files read this one table. The point of the example is the
-- package-level machinery — one Querier over several files, a shared row
-- struct declared once, a narrowed genericConn — rather than the type
-- vocabulary, which example/clickhouse_cdr covers.
--
-- ended_at and tag_sets are here for the import machinery rather than the
-- vocabulary: their Go types, *time.Time and []map[string]string, reach the
-- generated file only through a wrapper that carries no import of its own.
CREATE TABLE call_record (
    msisdn_a    String,
    record_type Enum8('MOC' = 1, 'SMO' = 2, 'GPRS' = 7),
    provider    LowCardinality(String),
    units       Int64,
    ended_at    Nullable(DateTime('UTC')),
    tag_sets    Array(Map(String, String))
) ENGINE = MergeTree()
ORDER BY (provider, msisdn_a);
