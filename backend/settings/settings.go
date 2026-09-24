package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	"voltis/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const channel = "settings_changed"

type Store struct {
	pool    *pgxpool.Pool
	mu      sync.RWMutex
	values  map[string]any
	version int64
	hooks   []func([]string)
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New loads a snapshot. Call Listen to keep it current.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := seedFromEnv(ctx, pool); err != nil {
		return nil, err
	}
	s := &Store{pool: pool, values: defaults()}
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// OnChange hooks run after every version advance and must not call Set.
func (s *Store) OnChange(fn func(keys []string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = append(s.hooks, fn)
}

func (s *Store) Bool(key string) bool {
	v, _ := s.Get(key).(bool)
	return v
}

func (s *Store) Int(key string) int {
	v, _ := s.Get(key).(int)
	return v
}

func (s *Store) String(key string) string {
	v, _ := s.Get(key).(string)
	return v
}

func (s *Store) StringList(key string) []string {
	v, _ := s.Get(key).([]string)
	return slices.Clone(v)
}

// Values returns a coherent snapshot: reading keys one by one can mix a
// reload's before and after.
func (s *Store) Values() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return maps.Clone(s.values)
}

func (s *Store) Get(key string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values[key]
}

func (s *Store) Version() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *Store) Set(ctx context.Context, key string, value any) error {
	return s.SetMany(ctx, map[string]any{key: value})
}

func (s *Store) SetMany(ctx context.Context, values map[string]any) error {
	err := db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		for _, key := range slices.Sorted(maps.Keys(values)) {
			if err := WriteTx(ctx, tx, key, values[key]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.Reload(ctx)
}

func (s *Store) Reload(ctx context.Context) error {
	version, values, err := load(ctx, s.pool)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if version < s.version {
		s.mu.Unlock()
		return nil
	}
	advanced := version > s.version
	var changed []string
	for _, d := range defs {
		if !reflect.DeepEqual(s.values[d.Key], values[d.Key]) {
			changed = append(changed, d.Key)
		}
	}
	s.values, s.version = values, version
	hooks := slices.Clone(s.hooks)
	s.mu.Unlock()

	// A value written twice between two reloads looks unchanged here, while its
	// transition already revoked sessions.
	if advanced || len(changed) > 0 {
		for _, fn := range hooks {
			fn(changed)
		}
	}
	return nil
}

func (s *Store) Listen() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Go(func() {
		for ctx.Err() == nil {
			if err := s.listen(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("[settings] listener stopped", "err", err)
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
				}
			}
		}
	})
}

func (s *Store) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Store) listen(ctx context.Context) error {
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return err
	}
	// A write may have happened while the listener was down.
	if err := s.Reload(ctx); err != nil {
		return err
	}
	for {
		if _, err := conn.WaitForNotification(ctx); err != nil {
			return err
		}
		if err := s.Reload(ctx); err != nil {
			return err
		}
	}
}

func Write(ctx context.Context, pool *pgxpool.Pool, key string, value any) error {
	return db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		return WriteTx(ctx, tx, key, value)
	})
}

func WriteTx(ctx context.Context, tx pgx.Tx, key string, value any) error {
	def, ok := byKey[key]
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	parsed, err := def.Parse(value)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(parsed)
	if err != nil {
		return err
	}

	// Bump first: it serializes writers, so old cannot change under us.
	if err := bumpVersion(ctx, tx); err != nil {
		return err
	}
	old, err := Read(ctx, tx, key)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, key, raw); err != nil {
		return err
	}

	if hook := transitions[key]; hook != nil && !reflect.DeepEqual(old, parsed) {
		if err := hook(ctx, tx, parsed); err != nil {
			return err
		}
	}
	return notify(ctx, tx)
}

func bumpVersion(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "UPDATE settings_version SET version = version + 1")
	return err
}

// LockVersion blocks until no settings write is in flight, and keeps any new
// one waiting until the caller's transaction ends. Use LockVersionWrite when
// the transaction will itself write a setting: upgrading share to exclusive
// deadlocks against a waiting writer.
func LockVersion(ctx context.Context, tx pgx.Tx) error {
	return lockVersion(ctx, tx, "SHARE")
}

func LockVersionWrite(ctx context.Context, tx pgx.Tx) error {
	return lockVersion(ctx, tx, "UPDATE")
}

func lockVersion(ctx context.Context, tx pgx.Tx, mode string) error {
	_, err := db.SelectScalar[int64](ctx, tx, "SELECT version FROM settings_version FOR "+mode)
	return err
}

// Postgres collapses identical payloads within a transaction, so a patch of
// many keys wakes listeners once.
func notify(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_notify($1, '')", channel)
	return err
}

// Read falls back to the key's default when no row is stored.
func Read(ctx context.Context, q db.Querier, key string) (any, error) {
	def, ok := byKey[key]
	if !ok {
		return nil, fmt.Errorf("unknown setting %q", key)
	}
	raw, err := db.SelectScalar[json.RawMessage](ctx, q, "SELECT value FROM settings WHERE key = $1", key)
	if errors.Is(err, pgx.ErrNoRows) {
		return def.Default, nil
	}
	if err != nil {
		return nil, err
	}
	return decode(def, raw)
}

func load(ctx context.Context, pool *pgxpool.Pool) (int64, map[string]any, error) {
	var version int64
	var stored map[string]json.RawMessage
	err := pool.QueryRow(ctx, `
		SELECT v.version, COALESCE((SELECT jsonb_object_agg(key, value) FROM settings), '{}'::jsonb)
		FROM settings_version v
	`).Scan(&version, &stored)
	if err != nil {
		return 0, nil, err
	}

	values := defaults()
	for key, raw := range stored {
		def, ok := byKey[key]
		if !ok {
			continue
		}
		v, err := decode(def, raw)
		if err != nil {
			slog.Warn("[settings] ignoring invalid stored value", "key", key, "err", err)
			continue
		}
		values[key] = v
	}
	return version, values, nil
}

func decode(def Def, raw json.RawMessage) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return def.Parse(v)
}

func defaults() map[string]any {
	m := make(map[string]any, len(defs))
	for _, d := range defs {
		m[d.Key] = d.Default
	}
	return m
}

func seedFromEnv(ctx context.Context, pool *pgxpool.Pool) error {
	raw, ok := os.LookupEnv("APP_REGISTRATION_ENABLED")
	if !ok {
		return nil
	}
	return db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		// Same lock order as WriteTx: version row first, then the setting.
		if err := LockVersionWrite(ctx, tx); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			"INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO NOTHING",
			AuthRegistrationEnabled, strconv.FormatBool(raw == "true"))
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if err := bumpVersion(ctx, tx); err != nil {
			return err
		}
		return notify(ctx, tx)
	})
}
