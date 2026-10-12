// Command valence-admin runs admin tasks from the command line.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/pkg/blob"
	"github.com/ACM-VIT/valence/services/api/internal/problems"
)

const usage = `usage:
  valence-admin problem validate <dir>...
  valence-admin problem import <dir>...`

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 3 || args[0] != "problem" {
		return errors.New(usage)
	}
	cmd, dirs := args[1], args[2:]
	if cmd != "validate" && cmd != "import" {
		return errors.New(usage)
	}

	// Validate every package before importing any, so a bad one
	// doesn't leave the database half seeded.
	pkgs, err := loadAll(dirs)
	if err != nil || cmd == "validate" {
		return err
	}
	return importAll(ctx, pkgs)
}

func loadAll(dirs []string) ([]*problems.Package, error) {
	var pkgs []*problems.Package
	bad := 0
	for _, dir := range dirs {
		p, err := problems.Load(dir)
		if err != nil {
			bad++
			fmt.Printf("%s: invalid\n  %s\n", dir, strings.ReplaceAll(err.Error(), "\n", "\n  "))
			continue
		}
		fmt.Printf("%s: ok\n", dir)
		pkgs = append(pkgs, p)
	}
	if bad > 0 {
		return nil, fmt.Errorf("%d of %d packages are invalid", bad, len(dirs))
	}
	return pkgs, nil
}

func importAll(ctx context.Context, pkgs []*problems.Package) error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	blobDir := os.Getenv("BLOB_DIR")
	if blobDir == "" {
		blobDir = ".data/blobs"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	blobs, err := blob.NewLocal(blobDir)
	if err != nil {
		return err
	}
	store := problems.NewStore(pool)

	for _, p := range pkgs {
		result, revision, err := store.Import(ctx, blobs, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Slug, err)
		}
		fmt.Printf("%s: %s (revision %d)\n", p.Slug, result, revision)
	}
	return nil
}
