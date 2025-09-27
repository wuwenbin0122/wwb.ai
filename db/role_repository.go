package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wuwenbin0122/wwb.ai/db/models"
)

// GetRoleByID fetches a single role record including extended metadata columns.
func GetRoleByID(ctx context.Context, pool *pgxpool.Pool, id int64) (*models.Role, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is nil")
	}

	var role models.Role
	const queryExt = `SELECT id, name, domain, tags, bio, personality, background, languages, skills FROM roles WHERE id = $1`
	if err := pool.QueryRow(ctx, queryExt, id).Scan(
		&role.ID,
		&role.Name,
		&role.Domain,
		&role.Tags,
		&role.Bio,
		&role.Personality,
		&role.Background,
		&role.Languages,
		&role.Skills,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UndefinedColumn {
			// Fallback to legacy schema without extended columns
			const queryLegacy = `SELECT id, name, domain, tags, bio FROM roles WHERE id = $1`
			if err2 := pool.QueryRow(ctx, queryLegacy, id).Scan(
				&role.ID,
				&role.Name,
				&role.Domain,
				&role.Tags,
				&role.Bio,
			); err2 != nil {
				return nil, fmt.Errorf("query role by id (legacy): %w", err2)
			}
		} else {
			return nil, fmt.Errorf("query role by id: %w", err)
		}
	}

	return &role, nil
}
