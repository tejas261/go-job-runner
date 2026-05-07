package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository[T any] struct {
	DB    *pgxpool.Pool
	Table string
}

func NewRepository[T any](db *pgxpool.Pool, table string) *Repository[T] {
	return &Repository[T]{DB: db, Table: table}
}

// Insert a row and return the generated ID
func (r *Repository[T]) CreateAndReturnID(ctx context.Context, columns []string, values []any) (string, error) {
	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) RETURNING id",
		r.Table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	var id string
	err := r.DB.QueryRow(ctx, query, values...).Scan(&id)
	return id, err
}

// Insert a row, no return value
func (r *Repository[T]) Create(ctx context.Context, columns []string, values []any) error {
	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		r.Table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	_, err := r.DB.Exec(ctx, query, values...)
	return err
}

// Find one row by ID, using a scanner function you provide
func (r *Repository[T]) FindByID(ctx context.Context, id string, scanner func(pgx.Row) (T, error)) (T, error) {
	query := fmt.Sprintf("SELECT * FROM %s WHERE id = $1", r.Table)
	row := r.DB.QueryRow(ctx, query, id)
	return scanner(row)
}


func (r *Repository[T]) FindRowsByColumn(ctx context.Context, targetColumn string, targetValue any, returnColumns []string) (pgx.Rows, error) {
	if len(returnColumns) == 0 {
		return nil, fmt.Errorf("returnColumns cannot be empty")
	}

	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = $1",
		strings.Join(returnColumns, ", "),
		r.Table,
		targetColumn,
	)

	return r.DB.Query(ctx, query, targetValue)
}

// Update specific columns by ID
func (r *Repository[T]) UpdateByID(ctx context.Context, id string, columns []string, values []any) error {
	setClauses := make([]string, len(columns))
	for i, col := range columns {
		setClauses[i] = fmt.Sprintf("%s = $%d", col, i+1)
	}

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE id = $%d",
		r.Table,
		strings.Join(setClauses, ", "),
		len(values)+1,
	)

	_, err := r.DB.Exec(ctx, query, append(values, id)...)
	return err
}

func (r *Repository[T]) UpdateByColumn(ctx context.Context, column string, value any, setColumns []string, setValues []any) error {
	if len(setColumns) == 0 {
		return fmt.Errorf("setColumns cannot be empty")
	}
	if len(setColumns) != len(setValues) {
		return fmt.Errorf("setColumns and setValues length mismatch")
	}

	setClauses := make([]string, len(setColumns))
	for i, col := range setColumns {
		setClauses[i] = fmt.Sprintf("%s = $%d", col, i+1)
	}

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s = $%d",
		r.Table,
		strings.Join(setClauses, ", "),
		column,
		len(setValues)+1,
	)

	args := append(setValues, value)
	_, err := r.DB.Exec(ctx, query, args...)
	return err
}
