package cmd

import (
	"context"
	"fmt"
	"strings"

	"voltis/settings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ListSettings(ctx context.Context, pool *pgxpool.Pool) error {
	store, err := settings.New(ctx, pool)
	if err != nil {
		return err
	}
	for _, def := range settings.All() {
		if def.Internal {
			continue
		}
		fmt.Printf("%-32s %s\n", def.Key, formatSetting(def, store.Get(def.Key)))
	}
	return nil
}

func GetSetting(ctx context.Context, pool *pgxpool.Pool, key string) error {
	def, err := publicSetting(key)
	if err != nil {
		return err
	}
	store, err := settings.New(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Println(formatSetting(def, store.Get(key)))
	return nil
}

func SetSetting(ctx context.Context, pool *pgxpool.Pool, key, value string) error {
	def, err := publicSetting(key)
	if err != nil {
		return err
	}
	parsed, err := def.ParseString(value)
	if err != nil {
		return err
	}
	if err := settings.Write(ctx, pool, key, parsed); err != nil {
		return err
	}
	fmt.Printf("%s = %s\n", key, formatSetting(def, parsed))
	return nil
}

func publicSetting(key string) (settings.Def, error) {
	def, ok := settings.Lookup(key)
	if !ok || def.Internal {
		return settings.Def{}, fmt.Errorf("unknown setting %q", key)
	}
	return def, nil
}

func formatSetting(def settings.Def, value any) string {
	if def.Secret() {
		if value == "" {
			return "<not set>"
		}
		return "<set>"
	}
	if l, ok := value.([]string); ok {
		return strings.Join(l, " ")
	}
	return fmt.Sprintf("%v", value)
}
