// Code generated by pggen. DO NOT EDIT.

package order

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
	FindOrdersByCustomer(ctx context.Context, customerID int32) ([]FindOrdersByCustomerRow, error)
	// FindOrdersByCustomerBatch enqueues a FindOrdersByCustomer query into batch to be executed
	// later by the batch.
	FindOrdersByCustomerBatch(ctx context.Context, batch *pgx.Batch, customerID int32)
	// FindOrdersByCustomerScan scans the result of an executed FindOrdersByCustomerBatch query.
	FindOrdersByCustomerScan(results pgx.BatchResults) ([]FindOrdersByCustomerRow, error)

	FindProductsInOrder(ctx context.Context, orderID int32) ([]FindProductsInOrderRow, error)
	// FindProductsInOrderBatch enqueues a FindProductsInOrder query into batch to be executed
	// later by the batch.
	FindProductsInOrderBatch(ctx context.Context, batch *pgx.Batch, orderID int32)
	// FindProductsInOrderScan scans the result of an executed FindProductsInOrderBatch query.
	FindProductsInOrderScan(results pgx.BatchResults) ([]FindProductsInOrderRow, error)
}

type DBQuerier struct {
	conn genericConn
}

var _ Querier = &DBQuerier{}

// genericConn is a connection to a Postgres database. This is usually backed by
// *pgx.Conn, pgx.Tx, or *pgxpool.Pool.
type genericConn interface {
	// Query executes sql with args. If there is an error the returned Rows will
	// be returned in an error state. So it is allowed to ignore the error
	// returned from Query and handle it in Rows.
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)

	// QueryRow is a convenience wrapper over Query. Any error that occurs while
	// querying is deferred until calling Scan on the returned Row. That Row will
	// error with pgx.ErrNoRows if no rows are returned.
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row

	// Exec executes sql. sql can be either a prepared statement name or an SQL
	// string. arguments should be referenced positionally from the sql string
	// as $1, $2, etc.
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
}

// NewQuerier creates a DBQuerier that implements Querier. conn is typically
// *pgx.Conn, pgx.Tx, or *pgxpool.Pool.
func NewQuerier(conn genericConn) *DBQuerier {
	return &DBQuerier{
		conn: conn,
	}
}

// WithTx creates a new DBQuerier that uses the transaction to run all queries.
func (q *DBQuerier) WithTx(tx pgx.Tx) (*DBQuerier, error) {
	return &DBQuerier{conn: tx}, nil
}

const findOrdersByCustomerSQL = `SELECT * FROM orders WHERE customer_id = $1;`

type FindOrdersByCustomerRow struct {
	OrderID    int32              `json:"order_id"`
	OrderDate  pgtype.Timestamptz `json:"order_date"`
	OrderTotal pgtype.Numeric     `json:"order_total"`
	CustomerID pgtype.Int4        `json:"customer_id"`
}

// FindOrdersByCustomer implements Querier.FindOrdersByCustomer.
func (q *DBQuerier) FindOrdersByCustomer(ctx context.Context, customerID int32) ([]FindOrdersByCustomerRow, error) {
	rows, err := q.conn.Query(ctx, findOrdersByCustomerSQL, customerID)
	if rows != nil {
		defer rows.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("query FindOrdersByCustomer: %w", err)
	}
	items := []FindOrdersByCustomerRow{}
	for rows.Next() {
		var item FindOrdersByCustomerRow
		if err := rows.Scan(&item.OrderID, &item.OrderDate, &item.OrderTotal, &item.CustomerID); err != nil {
			return nil, fmt.Errorf("scan FindOrdersByCustomer row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, err
}

// FindOrdersByCustomerBatch implements Querier.FindOrdersByCustomerBatch.
func (q *DBQuerier) FindOrdersByCustomerBatch(ctx context.Context, batch *pgx.Batch, customerID int32) {
	batch.Queue(findOrdersByCustomerSQL, customerID)
}

// FindOrdersByCustomerScan implements Querier.FindOrdersByCustomerScan.
func (q *DBQuerier) FindOrdersByCustomerScan(results pgx.BatchResults) ([]FindOrdersByCustomerRow, error) {
	rows, err := results.Query()
	if rows != nil {
		defer rows.Close()
	}
	if err != nil {
		return nil, err
	}
	items := []FindOrdersByCustomerRow{}
	for rows.Next() {
		var item FindOrdersByCustomerRow
		if err := rows.Scan(&item.OrderID, &item.OrderDate, &item.OrderTotal, &item.CustomerID); err != nil {
			return nil, fmt.Errorf("scan FindOrdersByCustomerBatch row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, err
}

const findProductsInOrderSQL = `SELECT o.order_id, p.product_id, p.name
FROM orders o
  INNER JOIN order_product op USING (order_id)
  INNER JOIN product p USING (product_id)
WHERE o.order_id = $1;`

type FindProductsInOrderRow struct {
	OrderID   pgtype.Int4 `json:"order_id"`
	ProductID pgtype.Int4 `json:"product_id"`
	Name      pgtype.Text `json:"name"`
}

// FindProductsInOrder implements Querier.FindProductsInOrder.
func (q *DBQuerier) FindProductsInOrder(ctx context.Context, orderID int32) ([]FindProductsInOrderRow, error) {
	rows, err := q.conn.Query(ctx, findProductsInOrderSQL, orderID)
	if rows != nil {
		defer rows.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("query FindProductsInOrder: %w", err)
	}
	items := []FindProductsInOrderRow{}
	for rows.Next() {
		var item FindProductsInOrderRow
		if err := rows.Scan(&item.OrderID, &item.ProductID, &item.Name); err != nil {
			return nil, fmt.Errorf("scan FindProductsInOrder row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, err
}

// FindProductsInOrderBatch implements Querier.FindProductsInOrderBatch.
func (q *DBQuerier) FindProductsInOrderBatch(ctx context.Context, batch *pgx.Batch, orderID int32) {
	batch.Queue(findProductsInOrderSQL, orderID)
}

// FindProductsInOrderScan implements Querier.FindProductsInOrderScan.
func (q *DBQuerier) FindProductsInOrderScan(results pgx.BatchResults) ([]FindProductsInOrderRow, error) {
	rows, err := results.Query()
	if rows != nil {
		defer rows.Close()
	}
	if err != nil {
		return nil, err
	}
	items := []FindProductsInOrderRow{}
	for rows.Next() {
		var item FindProductsInOrderRow
		if err := rows.Scan(&item.OrderID, &item.ProductID, &item.Name); err != nil {
			return nil, fmt.Errorf("scan FindProductsInOrderBatch row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, err
}
