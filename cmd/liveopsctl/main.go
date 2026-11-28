package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kinstall/kinstall/internal/bootstrap"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
)

func main() {
	defaultDir := bootstrap.DefaultDataDir()
	var dbDriver, dbDSN, keyringPath string

	root := &cobra.Command{
		Use:   "liveopsctl",
		Short: "LiveOps Foundation Governance & Cryptographic CLI",
	}
	root.PersistentFlags().StringVar(&dbDriver, "db-driver", "sqlite", "Database driver (sqlite | postgres)")
	root.PersistentFlags().StringVar(&dbDSN, "db-dsn", filepath.Join(defaultDir, "liveops.db"), "Database DSN")
	root.PersistentFlags().StringVar(&keyringPath, "keyring", filepath.Join(defaultDir, "keyring.json"), "KMS keyring path")

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print liveopsctl build version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(bootstrap.Version)
		},
	})

	secretCmd := &cobra.Command{Use: "secret", Short: "Envelope encryption operations (reads plaintext strictly from stdin)"}
	var aad string
	var useSM4 bool
	sealCmd := &cobra.Command{
		Use:   "seal",
		Short: "Seal plaintext from stdin into v2 envelope ciphertext",
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			pt := strings.TrimRight(string(raw), "\r\n")
			if pt == "" {
				return fmt.Errorf("stdin plaintext is empty")
			}
			codec, err := crypto.LoadOrCreateKeyring(keyringPath)
			if err != nil {
				return err
			}
			alg := crypto.AlgAES256GCM
			if useSM4 {
				alg = crypto.AlgSM4GCM
			}
			ct, err := codec.SealWithAlg([]byte(pt), []byte(aad), alg, 0)
			if err != nil {
				return err
			}
			fmt.Println(ct)
			return nil
		},
	}
	sealCmd.Flags().StringVar(&aad, "aad", "", "Optional associated authenticated data")
	sealCmd.Flags().BoolVar(&useSM4, "sm4", false, "Use national-standard SM4-GCM instead of AES-256-GCM")

	openCmd := &cobra.Command{
		Use:   "open",
		Short: "Open v2 envelope ciphertext from stdin",
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			ct := strings.TrimSpace(string(raw))
			codec, err := crypto.LoadOrCreateKeyring(keyringPath)
			if err != nil {
				return err
			}
			pt, err := codec.Open(ct, []byte(aad))
			if err != nil {
				return err
			}
			fmt.Println(string(pt))
			return nil
		},
	}
	openCmd.Flags().StringVar(&aad, "aad", "", "Associated authenticated data used when sealing")
	secretCmd.AddCommand(sealCmd, openCmd)
	root.AddCommand(secretCmd)

	dbCmd := &cobra.Command{Use: "db", Short: "Database migration and bootstrap utilities"}
	dbCmd.AddCommand(&cobra.Command{
		Use:   "migrate",
		Short: "Apply embedded Goose migrations and seed baseline governance data",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := bootstrap.Build(cmd.Context(), bootstrap.Options{
				DBDriver: dbDriver, DBDSN: dbDSN, KeyringPath: keyringPath,
			})
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			fmt.Printf("migrated %s (%s) successfully\n", dbDriver, dbDSN)
			return nil
		},
	})
	root.AddCommand(dbCmd)

	auditCmd := &cobra.Command{Use: "audit", Short: "Tamper-evident audit chain verification"}
	auditCmd.AddCommand(&cobra.Command{
		Use:   "verify",
		Short: "Verify full HMAC-SHA256 + prev_hash audit chain integrity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := bootstrap.Build(context.Background(), bootstrap.Options{
				DBDriver: dbDriver, DBDSN: dbDSN, KeyringPath: keyringPath, SkipSeed: true,
			})
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()
			res, err := c.Identity.VerifyAuditChain(cmd.Context())
			if err != nil {
				return err
			}
			out, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(out))
			if !res.Valid {
				return fmt.Errorf("audit chain tampered at index %d: %s", res.BrokenAt, res.Reason)
			}
			return nil
		},
	})
	root.AddCommand(auditCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
