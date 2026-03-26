package lspapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

const (
	statusPendingLN  = "pending_ln"
	statusPendingRGB = "pending_rgb"
	statusCompleted  = "completed"
	statusExpired    = "expired"
	statusCanceled   = "canceled"
	statusFailed     = "failed"
)

type Store interface {
	Close() error
	InsertOnchainSend(ctx context.Context, userRGBInvoice, lspLNInvoice string, lnExpiresAt *time.Time) (int64, error)
	InsertLightningReceive(ctx context.Context, userLNInvoice, lspRGBInvoice, rgbAssetID string, batchTransferIdx int64, rgbExpiresAt *time.Time) (int64, error)
	ListOnchainPending(ctx context.Context, limit int) ([]OnchainSendRecord, error)
	ListLightningPending(ctx context.Context, limit int) ([]LightningReceiveRecord, error)
	UpdateOnchainStatus(ctx context.Context, id int64, status, lastErr string) error
	UpdateLightningStatus(ctx context.Context, id int64, status, lastErr string) error
}

type SQLStore struct {
	db     *sql.DB
	driver string
}

func NewStore(cfg Config) (*SQLStore, error) {
	driver := cfg.DatabaseDriver
	dsn := cfg.DatabaseURL
	if driver == "sqlite" {
		driver = "sqlite3"
	}
	if driver == "postgres" {
		driver = "postgres"
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}

	s := &SQLStore{db: db, driver: cfg.DatabaseDriver}
	if err := s.pingAndMigrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

func (s *SQLStore) pingAndMigrate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}

	if s.driver == "postgres" {
		_, err := s.db.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS onchain_send_mappings (
				id BIGSERIAL PRIMARY KEY,
				user_rgb_invoice TEXT NOT NULL UNIQUE,
				lsp_ln_invoice TEXT NOT NULL UNIQUE,
				status TEXT NOT NULL,
				ln_expires_at TIMESTAMPTZ NULL,
				last_error TEXT NULL,
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);
			CREATE TABLE IF NOT EXISTS lightning_receive_mappings (
				id BIGSERIAL PRIMARY KEY,
				user_ln_invoice TEXT NOT NULL UNIQUE,
				lsp_rgb_invoice TEXT NOT NULL UNIQUE,
				rgb_asset_id TEXT NOT NULL,
				batch_transfer_idx BIGINT NOT NULL,
				status TEXT NOT NULL,
				rgb_expires_at TIMESTAMPTZ NULL,
				last_error TEXT NULL,
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);
		`)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `
			ALTER TABLE lightning_receive_mappings ADD COLUMN IF NOT EXISTS rgb_asset_id TEXT;
			ALTER TABLE lightning_receive_mappings ADD COLUMN IF NOT EXISTS batch_transfer_idx BIGINT;
		`)
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS onchain_send_mappings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_rgb_invoice TEXT NOT NULL UNIQUE,
			lsp_ln_invoice TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL,
			ln_expires_at DATETIME NULL,
			last_error TEXT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS lightning_receive_mappings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_ln_invoice TEXT NOT NULL UNIQUE,
			lsp_rgb_invoice TEXT NOT NULL UNIQUE,
			rgb_asset_id TEXT,
			batch_transfer_idx INTEGER,
			status TEXT NOT NULL,
			rgb_expires_at DATETIME NULL,
			last_error TEXT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}
	if err := s.tryAddColumnSQLite(ctx, "lightning_receive_mappings", "rgb_asset_id TEXT"); err != nil {
		return err
	}
	return s.tryAddColumnSQLite(ctx, "lightning_receive_mappings", "batch_transfer_idx INTEGER")
}

func (s *SQLStore) InsertOnchainSend(ctx context.Context, userRGBInvoice, lspLNInvoice string, lnExpiresAt *time.Time) (int64, error) {
	if s.driver == "postgres" {
		var id int64
		err := s.db.QueryRowContext(ctx, `
			INSERT INTO onchain_send_mappings (user_rgb_invoice, lsp_ln_invoice, status, ln_expires_at)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, userRGBInvoice, lspLNInvoice, statusPendingLN, lnExpiresAt).Scan(&id)
		return id, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO onchain_send_mappings (user_rgb_invoice, lsp_ln_invoice, status, ln_expires_at)
		VALUES (?, ?, ?, ?)
	`, userRGBInvoice, lspLNInvoice, statusPendingLN, lnExpiresAt)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *SQLStore) InsertLightningReceive(ctx context.Context, userLNInvoice, lspRGBInvoice, rgbAssetID string, batchTransferIdx int64, rgbExpiresAt *time.Time) (int64, error) {
	if s.driver == "postgres" {
		var id int64
		err := s.db.QueryRowContext(ctx, `
			INSERT INTO lightning_receive_mappings (user_ln_invoice, lsp_rgb_invoice, rgb_asset_id, batch_transfer_idx, status, rgb_expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`, userLNInvoice, lspRGBInvoice, rgbAssetID, batchTransferIdx, statusPendingRGB, rgbExpiresAt).Scan(&id)
		return id, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO lightning_receive_mappings (user_ln_invoice, lsp_rgb_invoice, rgb_asset_id, batch_transfer_idx, status, rgb_expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, userLNInvoice, lspRGBInvoice, rgbAssetID, batchTransferIdx, statusPendingRGB, rgbExpiresAt)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *SQLStore) ListOnchainPending(ctx context.Context, limit int) ([]OnchainSendRecord, error) {
	query := `SELECT id, user_rgb_invoice, lsp_ln_invoice, status, ln_expires_at, created_at FROM onchain_send_mappings WHERE status = ? ORDER BY id ASC LIMIT ?`
	args := []any{statusPendingLN, limit}
	if s.driver == "postgres" {
		query = `SELECT id, user_rgb_invoice, lsp_ln_invoice, status, ln_expires_at, created_at FROM onchain_send_mappings WHERE status = $1 ORDER BY id ASC LIMIT $2`
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]OnchainSendRecord, 0)
	for rows.Next() {
		var r OnchainSendRecord
		var lnExpires sql.NullTime
		if err := rows.Scan(&r.ID, &r.UserRGBInvoice, &r.LspLNInvoice, &r.Status, &lnExpires, &r.CreatedAt); err != nil {
			return nil, err
		}
		if lnExpires.Valid {
			t := lnExpires.Time
			r.LNExpiresAt = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLStore) ListLightningPending(ctx context.Context, limit int) ([]LightningReceiveRecord, error) {
	query := `SELECT id, user_ln_invoice, lsp_rgb_invoice, rgb_asset_id, batch_transfer_idx, status, rgb_expires_at, created_at FROM lightning_receive_mappings WHERE status = ? ORDER BY id ASC LIMIT ?`
	args := []any{statusPendingRGB, limit}
	if s.driver == "postgres" {
		query = `SELECT id, user_ln_invoice, lsp_rgb_invoice, rgb_asset_id, batch_transfer_idx, status, rgb_expires_at, created_at FROM lightning_receive_mappings WHERE status = $1 ORDER BY id ASC LIMIT $2`
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]LightningReceiveRecord, 0)
	for rows.Next() {
		var r LightningReceiveRecord
		var rgbExpires sql.NullTime
		if err := rows.Scan(&r.ID, &r.UserLNInvoice, &r.LspRGBInvoice, &r.RGBAssetID, &r.BatchTransferIdx, &r.Status, &rgbExpires, &r.CreatedAt); err != nil {
			return nil, err
		}
		if rgbExpires.Valid {
			t := rgbExpires.Time
			r.RGBExpiresAt = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLStore) UpdateOnchainStatus(ctx context.Context, id int64, status, lastErr string) error {
	if status == "" {
		return errors.New("empty status")
	}
	if s.driver == "postgres" {
		_, err := s.db.ExecContext(ctx, `UPDATE onchain_send_mappings SET status=$1, last_error=$2, updated_at=NOW() WHERE id=$3`, status, nullIfEmpty(lastErr), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE onchain_send_mappings SET status=?, last_error=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, nullIfEmpty(lastErr), id)
	return err
}

func (s *SQLStore) UpdateLightningStatus(ctx context.Context, id int64, status, lastErr string) error {
	if status == "" {
		return errors.New("empty status")
	}
	if s.driver == "postgres" {
		_, err := s.db.ExecContext(ctx, `UPDATE lightning_receive_mappings SET status=$1, last_error=$2, updated_at=NOW() WHERE id=$3`, status, nullIfEmpty(lastErr), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE lightning_receive_mappings SET status=?, last_error=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, nullIfEmpty(lastErr), id)
	return err
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func wrapErr(msg string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

func (s *SQLStore) tryAddColumnSQLite(ctx context.Context, table, columnDef string) error {
	if s.driver == "postgres" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, columnDef))
	if err == nil || strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return nil
	}
	return err
}
