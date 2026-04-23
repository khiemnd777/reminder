package repository

import (
	"context"
	"path/filepath"
	"testing"

	"reminder/backend/internal/model"
	"reminder/backend/pkg/db"
)

func TestAccountRepositoryUpsertAndList(t *testing.T) {
	t.Parallel()

	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()

	repo := NewAccountRepository(database)
	ctx := context.Background()

	if err := repo.Upsert(ctx, model.Account{
		ID:           "google",
		Provider:     model.ProviderGoogle,
		AccessToken:  "token-1",
		RefreshToken: "refresh-1",
	}); err != nil {
		t.Fatalf("upsert google: %v", err)
	}

	if err := repo.Upsert(ctx, model.Account{
		ID:           "google-replaced",
		Provider:     model.ProviderGoogle,
		AccessToken:  "token-2",
		RefreshToken: "refresh-2",
	}); err != nil {
		t.Fatalf("upsert duplicate provider: %v", err)
	}

	accounts, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0].AccessToken != "token-2" || accounts[0].ID != "google-replaced" {
		t.Fatalf("unexpected account after upsert: %+v", accounts[0])
	}
}

func TestAccountRepositoryGetByProviderReturnsNilWhenMissing(t *testing.T) {
	t.Parallel()

	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()

	repo := NewAccountRepository(database)
	account, err := repo.GetByProvider(context.Background(), model.ProviderApple)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	if account != nil {
		t.Fatalf("expected nil account, got %+v", account)
	}
}
