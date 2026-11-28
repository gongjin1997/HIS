package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/kinstall/kinstall/internal/adapter/store"
	"github.com/kinstall/kinstall/internal/usecase/assets"
	"github.com/kinstall/kinstall/internal/usecase/config"
	"github.com/kinstall/kinstall/internal/usecase/deploy"
	"github.com/kinstall/kinstall/internal/usecase/gateway"
	"github.com/kinstall/kinstall/internal/usecase/identity"
	"github.com/kinstall/kinstall/internal/usecase/nodes"
	"github.com/kinstall/kinstall/migrations"
	"github.com/kinstall/kinstall/pkg/platform/audit"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

const Version = "liveops-foundation/1.0.0"

type Options struct {
	DBDriver    string
	DBDSN       string
	KeyringPath string
	Clock       id.Clock
	SkipSeed    bool
}

type Container struct {
	Logger   *slog.Logger
	DB       *db.Store
	Codec    *crypto.EnvelopeCodec
	Repo     *store.Repo
	Tokens   *auth.TokenManager
	GWSecret []byte
	Identity *identity.Service
	Assets   *assets.Service
	Config   *config.Service
	Deploy   *deploy.Service
	Nodes    *nodes.Service
	Gateway  *gateway.Service
}

func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".liveops"
	}
	return filepath.Join(home, ".liveops")
}

func Build(ctx context.Context, opts Options) (*Container, error) {
	logger := slog.New(audit.NewRedactingHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if opts.DBDriver == "" {
		opts.DBDriver = "sqlite"
	}
	if opts.DBDSN == "" && opts.DBDriver == "sqlite" {
		opts.DBDSN = filepath.Join(DefaultDataDir(), "liveops.db")
	}
	if opts.KeyringPath == "" {
		opts.KeyringPath = filepath.Join(DefaultDataDir(), "keyring.json")
	}
	if opts.Clock == nil {
		opts.Clock = id.RealClock{}
	}

	codec, err := crypto.LoadOrCreateKeyring(opts.KeyringPath)
	if err != nil {
		return nil, fmt.Errorf("load keyring: %w", err)
	}

	dbStore, err := db.Open(ctx, db.Config{Driver: opts.DBDriver, DSN: opts.DBDSN})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	migFS, err := migrations.ForDialect(dbStore.Dialect())
	if err != nil {
		_ = dbStore.Close()
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	if err := dbStore.Migrate(ctx, migFS); err != nil {
		_ = dbStore.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	// Derive domain-separated 32-byte HMAC secrets from the KMS master key (never from public material!).
	tokenKey := codec.DeriveHMACKey("jwt-session")
	auditKey := codec.DeriveHMACKey("audit-chain")
	gwSecret := codec.DeriveHMACKey("gateway-internal")

	repo := store.New(dbStore, opts.Clock, codec, auditKey)
	if !opts.SkipSeed {
		if err := repo.Seed(ctx); err != nil {
			_ = dbStore.Close()
			return nil, fmt.Errorf("seed db: %w", err)
		}
	}

	tokens := auth.NewTokenManager(tokenKey, 12*time.Hour)
	hub := deploy.NewEventHub()

	return &Container{
		Logger:   logger,
		DB:       dbStore,
		Codec:    codec,
		Repo:     repo,
		Tokens:   tokens,
		GWSecret: gwSecret,
		Identity: identity.New(repo, tokens),
		Assets:   assets.New(repo),
		Config:   config.New(repo, codec),
		Deploy:   deploy.New(repo, hub),
		Nodes:    nodes.New(repo),
		Gateway:  gateway.New(repo),
	}, nil
}

func (c *Container) Close() error {
	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}
