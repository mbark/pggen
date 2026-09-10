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

-- The stand-in for the {cdr_source:Identifier} parameter below. chgen has to
-- resolve the query to learn its result columns, so it substitutes the
-- parameter's own name and describes the query against a table called
-- cdr_source. At run time the caller names a real table instead.
CREATE TABLE cdr_source AS cdr
ENGINE = MergeTree()
ORDER BY (provider, a_num, start_date);

-- A second table the runtime can point the query at, to show it moves.
CREATE TABLE cdr_archive AS cdr
ENGINE = MergeTree()
ORDER BY (provider, a_num, start_date);
