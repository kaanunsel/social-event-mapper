package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kaanunsel/social-event-mapper/backend/internal/domain"
)

// CategoryRepository is the Postgres-backed implementation of category.Repository.
type CategoryRepository struct {
	pool *pgxpool.Pool
}

// NewCategoryRepository returns a repository that executes queries against the given connection pool.
func NewCategoryRepository(pool *pgxpool.Pool) *CategoryRepository {
	return &CategoryRepository{pool: pool}
}

// ListCategories returns all event_category rows ordered by id ascending.
func (r *CategoryRepository) ListCategories(ctx context.Context) ([]domain.EventCategory, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name
		FROM event_category
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	var categories []domain.EventCategory
	for rows.Next() {
		var c domain.EventCategory
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		categories = append(categories, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}

	return categories, nil
}
