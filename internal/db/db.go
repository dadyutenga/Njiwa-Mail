// Package db opens a GORM session on PostgreSQL and applies embedded SQL migrations.
//
// The schema stays in SQL. Partial indexes, triggers, and the dm_emit /
// template-immutability functions are PostgreSQL objects GORM cannot emit, so
// AutoMigrate is not used. Application queries run on the GORM connection.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNoRows is returned by QueryRow.Scan when the query matches nothing.
var ErrNoRows = sql.ErrNoRows

// DB is a GORM session against PostgreSQL.
type DB struct {
	G *gorm.DB
}

// Open validates url, opens a GORM PostgreSQL session, and pings it.
func Open(ctx context.Context, url string) (*DB, error) {
	if _, err := pgx.ParseConfig(url); err != nil {
		return nil, fmt.Errorf("invalid DATABASE_URL")
	}
	g, err := gorm.Open(postgres.New(postgres.Config{
		DSN:        url,
		DriverName: "pgx",
	}), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
		NowFunc:                func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := g.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("database unreachable: %w", err)
	}
	return &DB{G: g}, nil
}

// Close releases the underlying connection pool.
func (d *DB) Close() {
	if d == nil || d.G == nil {
		return
	}
	if sqlDB, err := d.G.DB(); err == nil {
		sqlDB.Close()
	}
}

// Migrate applies pending SQL migrations under a session advisory lock.
func Migrate(ctx context.Context, d *DB) error {
	sqlDB, err := d.G.DB()
	if err != nil {
		return err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(dc any) error {
		pg := dc.(*stdlib.Conn).Conn()
		if _, err := pg.Exec(ctx, `SELECT pg_advisory_lock(727001)`); err != nil {
			return err
		}
		defer pg.Exec(context.Background(), `SELECT pg_advisory_unlock(727001)`)
		if _, err := pg.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return err
		}
		entries, err := migrationFS.ReadDir("migrations")
		if err != nil {
			return err
		}
		names := []string{}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".sql") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			var done bool
			if err := pg.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, n).Scan(&done); err != nil {
				return err
			}
			if done {
				continue
			}
			sqlText, err := migrationFS.ReadFile("migrations/" + n)
			if err != nil {
				return err
			}
			if _, err := pg.Exec(ctx, `BEGIN`); err != nil {
				return err
			}
			if _, err := pg.Exec(ctx, string(sqlText)); err != nil {
				pg.Exec(ctx, `ROLLBACK`)
				return fmt.Errorf("migration %s: %w", n, err)
			}
			if _, err := pg.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, n); err != nil {
				pg.Exec(ctx, `ROLLBACK`)
				return err
			}
			if _, err := pg.Exec(ctx, `COMMIT`); err != nil {
				return err
			}
		}
		return nil
	})
}

// Result is the outcome of Exec.
type Result struct{ n int64 }

// RowsAffected reports how many rows the statement changed.
func (r Result) RowsAffected() int64 { return r.n }

// Exec runs a statement on the GORM connection.
func (d *DB) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return exec(ctx, d.G.WithContext(ctx), query, args...)
}

// Query runs a query on the GORM connection.
func (d *DB) Query(ctx context.Context, query string, args ...any) (*Rows, error) {
	return queryRows(ctx, d.G.WithContext(ctx), query, args...)
}

// QueryRow runs a query expected to return at most one row.
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) Row {
	return queryRow(ctx, d.G.WithContext(ctx), query, args...)
}

// Begin starts a transaction on the GORM session.
func (d *DB) Begin(ctx context.Context) (*Tx, error) {
	g := d.G.WithContext(ctx).Begin()
	if g.Error != nil {
		return nil, g.Error
	}
	return &Tx{g: g}, nil
}

// Tx is a GORM transaction.
type Tx struct{ g *gorm.DB }

// Exec runs a statement inside the transaction.
func (t *Tx) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return exec(ctx, t.g, query, args...)
}

// Query runs a query inside the transaction.
func (t *Tx) Query(ctx context.Context, query string, args ...any) (*Rows, error) {
	return queryRows(ctx, t.g, query, args...)
}

// QueryRow runs a single-row query inside the transaction.
func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) Row {
	return queryRow(ctx, t.g, query, args...)
}

// Commit commits the transaction.
func (t *Tx) Commit(context.Context) error {
	if t == nil || t.g == nil {
		return errors.New("nil transaction")
	}
	return t.g.Commit().Error
}

// Rollback aborts the transaction. It is safe to call after Commit.
func (t *Tx) Rollback(context.Context) error {
	if t == nil || t.g == nil {
		return nil
	}
	return t.g.Rollback().Error
}

func connOf(g *gorm.DB) (gorm.ConnPool, error) {
	if g != nil && g.Statement != nil && g.Statement.ConnPool != nil {
		return g.Statement.ConnPool, nil
	}
	if g == nil {
		return nil, errors.New("nil gorm session")
	}
	sqlDB, err := g.DB()
	if err != nil {
		return nil, err
	}
	return sqlDB, nil
}

func exec(ctx context.Context, g *gorm.DB, query string, args ...any) (Result, error) {
	c, err := connOf(g)
	if err != nil {
		return Result{}, err
	}
	res, err := c.ExecContext(ctx, query, args...)
	if err != nil {
		return Result{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Result{}, err
	}
	return Result{n: n}, nil
}

func queryRows(ctx context.Context, g *gorm.DB, query string, args ...any) (*Rows, error) {
	c, err := connOf(g)
	if err != nil {
		return nil, err
	}
	rows, err := c.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{r: rows}, nil
}

func queryRow(ctx context.Context, g *gorm.DB, query string, args ...any) Row {
	c, err := connOf(g)
	if err != nil {
		return Row{err: err}
	}
	return Row{row: c.QueryRowContext(ctx, query, args...)}
}

// Rows is a query result. Scan accepts []string destinations for PostgreSQL text arrays.
type Rows struct{ r *sql.Rows }

// Next advances to the next row.
func (r *Rows) Next() bool { return r != nil && r.r != nil && r.r.Next() }

// Scan reads the current row.
func (r *Rows) Scan(dest ...any) error {
	if r == nil || r.r == nil {
		return sql.ErrNoRows
	}
	adapted, done := adaptDest(dest)
	if err := r.r.Scan(adapted...); err != nil {
		return err
	}
	done()
	return nil
}

// Close releases the row cursor.
func (r *Rows) Close() {
	if r != nil && r.r != nil {
		r.r.Close()
	}
}

// Err returns the iteration error.
func (r *Rows) Err() error {
	if r == nil || r.r == nil {
		return nil
	}
	return r.r.Err()
}

// Row is a single-row query result.
type Row struct {
	row *sql.Row
	err error
}

// Scan reads the row into dest.
func (r Row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.row == nil {
		return sql.ErrNoRows
	}
	adapted, done := adaptDest(dest)
	if err := r.row.Scan(adapted...); err != nil {
		return err
	}
	done()
	return nil
}

func adaptDest(dest []any) ([]any, func()) {
	out := make([]any, len(dest))
	var backs []func()
	for i, d := range dest {
		if p, ok := d.(*[]string); ok {
			tmp := new(textArray)
			out[i] = tmp
			backs = append(backs, func() { *p = []string(*tmp) })
			continue
		}
		out[i] = d
	}
	return out, func() {
		for _, f := range backs {
			f()
		}
	}
}
