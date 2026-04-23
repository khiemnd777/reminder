package repository

import (
	"context"
	"database/sql"
	"fmt"

	"reminder/backend/internal/model"
)

type AccountRepository struct {
	db *sql.DB
}

func NewAccountRepository(db *sql.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

func (r *AccountRepository) Upsert(ctx context.Context, account model.Account) error {
	query := `
INSERT INTO accounts (id, provider, access_token, refresh_token, extra)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(provider) DO UPDATE SET
  id = excluded.id,
  access_token = excluded.access_token,
  refresh_token = excluded.refresh_token,
  extra = excluded.extra
`
	if _, err := r.db.ExecContext(ctx, query, account.ID, account.Provider, account.AccessToken, account.RefreshToken, string(account.Extra)); err != nil {
		return fmt.Errorf("upsert account %s: %w", account.Provider, err)
	}
	return nil
}

func (r *AccountRepository) GetByProvider(ctx context.Context, provider string) (*model.Account, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, provider, access_token, refresh_token, extra
FROM accounts
WHERE provider = ?
`, provider)

	var account model.Account
	var extra string
	err := row.Scan(&account.ID, &account.Provider, &account.AccessToken, &account.RefreshToken, &extra)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get account %s: %w", provider, err)
	}
	account.Extra = []byte(extra)
	return &account, nil
}

func (r *AccountRepository) List(ctx context.Context) ([]model.Account, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, provider, access_token, refresh_token, extra
FROM accounts
ORDER BY provider ASC
`)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []model.Account
	for rows.Next() {
		var account model.Account
		var extra string
		if err := rows.Scan(&account.ID, &account.Provider, &account.AccessToken, &account.RefreshToken, &extra); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		account.Extra = []byte(extra)
		accounts = append(accounts, account)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate accounts: %w", err)
	}

	return accounts, nil
}
