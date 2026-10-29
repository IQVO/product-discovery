package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/IQVO/product-discovery/internal/application/ports"
	"github.com/IQVO/product-discovery/internal/domain/categoryview"
)

// CategoryRepository is the Postgres ports.CategoryRepository over the single
// row of category_view.
type CategoryRepository struct{ pool *pgxpool.Pool }

var _ ports.CategoryRepository = (*CategoryRepository)(nil)

// NewCategoryRepository constructs a CategoryRepository over pool.
func NewCategoryRepository(pool *pgxpool.Pool) *CategoryRepository {
	return &CategoryRepository{pool: pool}
}

type categoryJSON struct {
	ID       string `json:"categoryId"`
	ParentID string `json:"parentId,omitempty"`
	Name     string `json:"name"`
	Path     string `json:"path"`
}

// Current implements ports.CategoryRepository.
func (r *CategoryRepository) Current(ctx context.Context) (categoryview.View, error) {
	return r.read(ctx)
}

// CurrentForUpdate implements ports.CategoryRepository: the single row is
// locked FOR UPDATE inside a unit of work; before the first publication an
// advisory lock serialises the first writers.
func (r *CategoryRepository) CurrentForUpdate(ctx context.Context) (categoryview.View, error) {
	if !inUnitOfWork(ctx) {
		return r.read(ctx)
	}
	if _, err := queryFor(ctx, r.pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('category_view', 0))`); err != nil {
		return categoryview.View{}, fmt.Errorf("lock category view: %w", err)
	}
	return r.read(ctx)
}

func (r *CategoryRepository) read(ctx context.Context) (categoryview.View, error) {
	var version int64
	var raw []byte
	err := queryFor(ctx, r.pool).QueryRow(ctx, `SELECT version, categories FROM category_view WHERE singleton`).Scan(&version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return categoryview.View{}, nil
	}
	if err != nil {
		return categoryview.View{}, fmt.Errorf("read category view: %w", err)
	}
	var stored []categoryJSON
	if err := json.Unmarshal(raw, &stored); err != nil {
		return categoryview.View{}, fmt.Errorf("decode category view: %w", err)
	}
	cats := make([]categoryview.Category, len(stored))
	for i, c := range stored {
		cats[i] = categoryview.Category(c)
	}
	return categoryview.New(version, cats)
}

// Replace implements ports.CategoryRepository.
func (r *CategoryRepository) Replace(ctx context.Context, v categoryview.View) error {
	cats := v.Categories()
	stored := make([]categoryJSON, len(cats))
	for i, c := range cats {
		stored[i] = categoryJSON(c)
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("encode category view: %w", err)
	}
	_, err = queryFor(ctx, r.pool).Exec(ctx, `
		INSERT INTO category_view (singleton, version, categories) VALUES (true, $1, $2)
		ON CONFLICT (singleton) DO UPDATE SET version = EXCLUDED.version, categories = EXCLUDED.categories`, v.Version(), raw)
	if err != nil {
		return fmt.Errorf("replace category view: %w", err)
	}
	return nil
}
