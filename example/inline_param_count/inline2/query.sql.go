// Code generated by pggen. DO NOT EDIT.

package inline2

import (
	"context"
	"fmt"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
)

// Querier is a typesafe Go interface backed by SQL queries.
//
// Methods ending with Batch enqueue a query to run later in a pgx.Batch. After
// calling SendBatch on pgx.Conn, pgxpool.Pool, or pgx.Tx, use the Scan methods
// to parse the results.
type Querier interface {
	// CountAuthors returns the number of authors (zero params).
	CountAuthors(ctx context.Context) (*int, error)
	// CountAuthorsBatch enqueues a CountAuthors query into batch to be executed
	// later by the batch.
	CountAuthorsBatch(batch genericBatch)
	// CountAuthorsScan scans the result of an executed CountAuthorsBatch query.
	CountAuthorsScan(results pgx.BatchResults) (*int, error)

	// FindAuthorById finds one (or zero) authors by ID (one param).
	FindAuthorByID(ctx context.Context, authorID int32) (FindAuthorByIDRow, error)
	// FindAuthorByIDBatch enqueues a FindAuthorByID query into batch to be executed
	// later by the batch.
	FindAuthorByIDBatch(batch genericBatch, authorID int32)
	// FindAuthorByIDScan scans the result of an executed FindAuthorByIDBatch query.
	FindAuthorByIDScan(results pgx.BatchResults) (FindAuthorByIDRow, error)

	// InsertAuthor inserts an author by name and returns the ID (two params).
	InsertAuthor(ctx context.Context, firstName string, lastName string) (int32, error)
	// InsertAuthorBatch enqueues a InsertAuthor query into batch to be executed
	// later by the batch.
	InsertAuthorBatch(batch genericBatch, firstName string, lastName string)
	// InsertAuthorScan scans the result of an executed InsertAuthorBatch query.
	InsertAuthorScan(results pgx.BatchResults) (int32, error)

	// DeleteAuthorsByFullName deletes authors by the full name (three params).
	DeleteAuthorsByFullName(ctx context.Context, params DeleteAuthorsByFullNameParams) (pgconn.CommandTag, error)
	// DeleteAuthorsByFullNameBatch enqueues a DeleteAuthorsByFullName query into batch to be executed
	// later by the batch.
	DeleteAuthorsByFullNameBatch(batch genericBatch, params DeleteAuthorsByFullNameParams)
	// DeleteAuthorsByFullNameScan scans the result of an executed DeleteAuthorsByFullNameBatch query.
	DeleteAuthorsByFullNameScan(results pgx.BatchResults) (pgconn.CommandTag, error)
}

var _ Querier = &DBQuerier{}

type DBQuerier struct {
	conn  genericConn   // underlying Postgres transport to use
	types *typeResolver // resolve types by name
}

// genericConn is a connection like *pgx.Conn, pgx.Tx, or *pgxpool.Pool.
type genericConn interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// genericBatch batches queries to send in a single network request to a
// Postgres server. This is usually backed by *pgx.Batch.
type genericBatch interface {
	// Queue queues a query to batch b. query can be an SQL query or the name of a
	// prepared statement. See Queue on *pgx.Batch.
	Queue(query string, arguments ...interface{})
}

// NewQuerier creates a DBQuerier that implements Querier. conn is typically
// *pgx.Conn, pgx.Tx, or *pgxpool.Pool.
func NewQuerier(conn genericConn) *DBQuerier {
	return &DBQuerier{conn: conn, types: newTypeResolver()}
}

// typeResolver looks up the pgtype.ValueTranscoder by Postgres type name.
type typeResolver struct {
	connInfo *pgtype.ConnInfo // types by Postgres type name
}

func newTypeResolver() *typeResolver {
	ci := pgtype.NewConnInfo()
	return &typeResolver{connInfo: ci}
}

// findValue find the OID, and pgtype.ValueTranscoder for a Postgres type name.
func (tr *typeResolver) findValue(name string) (uint32, pgtype.ValueTranscoder, bool) {
	typ, ok := tr.connInfo.DataTypeForName(name)
	if !ok {
		return 0, nil, false
	}
	v := pgtype.NewValue(typ.Value)
	return typ.OID, v.(pgtype.ValueTranscoder), true
}

// setValue sets the value of a ValueTranscoder to a value that should always
// work and panics if it fails.
func (tr *typeResolver) setValue(vt pgtype.ValueTranscoder, val interface{}) pgtype.ValueTranscoder {
	if err := vt.Set(val); err != nil {
		panic(fmt.Sprintf("set ValueTranscoder %T to %+v: %s", vt, val, err))
	}
	return vt
}

const countAuthorsSQL = `SELECT count(*) FROM author;`

// CountAuthors implements Querier.CountAuthors.
func (q *DBQuerier) CountAuthors(ctx context.Context) (*int, error) {
	ctx = context.WithValue(ctx, "pggen_query_name", "CountAuthors")
	row := q.conn.QueryRow(ctx, countAuthorsSQL)
	var item *int
	if err := row.Scan(&item); err != nil {
		return item, fmt.Errorf("query CountAuthors: %w", err)
	}
	return item, nil
}

// CountAuthorsBatch implements Querier.CountAuthorsBatch.
func (q *DBQuerier) CountAuthorsBatch(batch genericBatch) {
	batch.Queue(countAuthorsSQL)
}

// CountAuthorsScan implements Querier.CountAuthorsScan.
func (q *DBQuerier) CountAuthorsScan(results pgx.BatchResults) (*int, error) {
	row := results.QueryRow()
	var item *int
	if err := row.Scan(&item); err != nil {
		return item, fmt.Errorf("scan CountAuthorsBatch row: %w", err)
	}
	return item, nil
}

const findAuthorByIDSQL = `SELECT * FROM author WHERE author_id = $1;`

type FindAuthorByIDRow struct {
	AuthorID  int32   `json:"author_id"`
	FirstName string  `json:"first_name"`
	LastName  string  `json:"last_name"`
	Suffix    *string `json:"suffix"`
}

// FindAuthorByID implements Querier.FindAuthorByID.
func (q *DBQuerier) FindAuthorByID(ctx context.Context, authorID int32) (FindAuthorByIDRow, error) {
	ctx = context.WithValue(ctx, "pggen_query_name", "FindAuthorByID")
	row := q.conn.QueryRow(ctx, findAuthorByIDSQL, authorID)
	var item FindAuthorByIDRow
	if err := row.Scan(&item.AuthorID, &item.FirstName, &item.LastName, &item.Suffix); err != nil {
		return item, fmt.Errorf("query FindAuthorByID: %w", err)
	}
	return item, nil
}

// FindAuthorByIDBatch implements Querier.FindAuthorByIDBatch.
func (q *DBQuerier) FindAuthorByIDBatch(batch genericBatch, authorID int32) {
	batch.Queue(findAuthorByIDSQL, authorID)
}

// FindAuthorByIDScan implements Querier.FindAuthorByIDScan.
func (q *DBQuerier) FindAuthorByIDScan(results pgx.BatchResults) (FindAuthorByIDRow, error) {
	row := results.QueryRow()
	var item FindAuthorByIDRow
	if err := row.Scan(&item.AuthorID, &item.FirstName, &item.LastName, &item.Suffix); err != nil {
		return item, fmt.Errorf("scan FindAuthorByIDBatch row: %w", err)
	}
	return item, nil
}

const insertAuthorSQL = `INSERT INTO author (first_name, last_name)
VALUES ($1, $2)
RETURNING author_id;`

// InsertAuthor implements Querier.InsertAuthor.
func (q *DBQuerier) InsertAuthor(ctx context.Context, firstName string, lastName string) (int32, error) {
	ctx = context.WithValue(ctx, "pggen_query_name", "InsertAuthor")
	row := q.conn.QueryRow(ctx, insertAuthorSQL, firstName, lastName)
	var item int32
	if err := row.Scan(&item); err != nil {
		return item, fmt.Errorf("query InsertAuthor: %w", err)
	}
	return item, nil
}

// InsertAuthorBatch implements Querier.InsertAuthorBatch.
func (q *DBQuerier) InsertAuthorBatch(batch genericBatch, firstName string, lastName string) {
	batch.Queue(insertAuthorSQL, firstName, lastName)
}

// InsertAuthorScan implements Querier.InsertAuthorScan.
func (q *DBQuerier) InsertAuthorScan(results pgx.BatchResults) (int32, error) {
	row := results.QueryRow()
	var item int32
	if err := row.Scan(&item); err != nil {
		return item, fmt.Errorf("scan InsertAuthorBatch row: %w", err)
	}
	return item, nil
}

const deleteAuthorsByFullNameSQL = `DELETE
FROM author
WHERE first_name = $1
  AND last_name = $2
  AND CASE WHEN $3 = '' THEN suffix IS NULL ELSE suffix = $3 END;`

type DeleteAuthorsByFullNameParams struct {
	FirstName string `json:"FirstName"`
	LastName  string `json:"LastName"`
	Suffix    string `json:"Suffix"`
}

// DeleteAuthorsByFullName implements Querier.DeleteAuthorsByFullName.
func (q *DBQuerier) DeleteAuthorsByFullName(ctx context.Context, params DeleteAuthorsByFullNameParams) (pgconn.CommandTag, error) {
	ctx = context.WithValue(ctx, "pggen_query_name", "DeleteAuthorsByFullName")
	cmdTag, err := q.conn.Exec(ctx, deleteAuthorsByFullNameSQL, params.FirstName, params.LastName, params.Suffix)
	if err != nil {
		return cmdTag, fmt.Errorf("exec query DeleteAuthorsByFullName: %w", err)
	}
	return cmdTag, err
}

// DeleteAuthorsByFullNameBatch implements Querier.DeleteAuthorsByFullNameBatch.
func (q *DBQuerier) DeleteAuthorsByFullNameBatch(batch genericBatch, params DeleteAuthorsByFullNameParams) {
	batch.Queue(deleteAuthorsByFullNameSQL, params.FirstName, params.LastName, params.Suffix)
}

// DeleteAuthorsByFullNameScan implements Querier.DeleteAuthorsByFullNameScan.
func (q *DBQuerier) DeleteAuthorsByFullNameScan(results pgx.BatchResults) (pgconn.CommandTag, error) {
	cmdTag, err := results.Exec()
	if err != nil {
		return cmdTag, fmt.Errorf("exec DeleteAuthorsByFullNameBatch: %w", err)
	}
	return cmdTag, err
}

// textPreferrer wraps a pgtype.ValueTranscoder and sets the preferred encoding
// format to text instead binary (the default). pggen uses the text format
// when the OID is unknownOID because the binary format requires the OID.
// Typically occurs for unregistered types.
type textPreferrer struct {
	pgtype.ValueTranscoder
	typeName string
}

// PreferredParamFormat implements pgtype.ParamFormatPreferrer.
func (t textPreferrer) PreferredParamFormat() int16 { return pgtype.TextFormatCode }

func (t textPreferrer) NewTypeValue() pgtype.Value {
	return textPreferrer{ValueTranscoder: pgtype.NewValue(t.ValueTranscoder).(pgtype.ValueTranscoder), typeName: t.typeName}
}

func (t textPreferrer) TypeName() string {
	return t.typeName
}

// unknownOID means we don't know the OID for a type. This is okay for decoding
// because pgx call DecodeText or DecodeBinary without requiring the OID. For
// encoding parameters, pggen uses textPreferrer if the OID is unknown.
const unknownOID = 0
