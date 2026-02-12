package db

import (
	"database/sql"
	"fmt"
)

// Scannable is an interface for types that can be scanned from SQL rows
type Scannable interface {
	Scan(rows *sql.Rows) error
}

// scanRows is a generic helper that scans SQL rows into a slice of pointers
func scanRows[T any, PT interface {
	*T
	Scannable
}](rows *sql.Rows) ([]*T, error) {
	var results []*T

	for rows.Next() {
		item := PT(new(T))
		if err := item.Scan(rows); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		results = append(results, (*T)(item))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}
