-- A cut-down call detail record table, modelled on the type vocabulary a real
-- ClickHouse schema uses: enums, LowCardinality, Nullable, Decimal, DateTime64,
-- arrays and maps.
CREATE TABLE cdr (
    a_num           String,
    record_type     Enum8('MOC' = 1, 'SMO' = 2, 'GPRS' = 7),
    rat             LowCardinality(Nullable(String)),
    provider        LowCardinality(String),
    units           Int64,
    charge          Decimal(18, 6),
    start_date      DateTime('UTC'),
    end_date        Nullable(DateTime('UTC')),
    updated_at      DateTime64(3, 'UTC'),
    labels          Array(String),
    metadata        Map(String, String),
    subscription_id UUID,
    customer_id     Nullable(UUID),
    premium         Bool
) ENGINE = MergeTree()
ORDER BY (provider, a_num, start_date);
