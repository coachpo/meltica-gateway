package unit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coachpo/meltica/internal/schema"
	"github.com/coachpo/meltica/internal/snapshot"
)

func TestMemoryStoreLifecycle(t *testing.T) {
	store := snapshot.NewMemoryStore()
	defer store.Close()

	key := snapshot.Key{Market: "BINANCE", Instrument: "BTC-USDT", Type: schema.CanonicalType("ORDERBOOK.SNAPSHOT")}
	record := snapshot.Record{Key: key, Seq: 1, Data: map[string]any{"bid": 1.0}, UpdatedAt: time.Now().UTC(), TTL: time.Millisecond}

	saved, err := store.Put(context.Background(), record)
	require.NoError(t, err)
	require.Equal(t, uint64(1), saved.Version)

	loaded, err := store.Get(context.Background(), key)
	require.NoError(t, err)
	require.Equal(t, record.Data["bid"], loaded.Data["bid"])

	updated := loaded
	updated.Seq = 2
	updated.Data["ask"] = 2.0

	swapped, err := store.CompareAndSwap(context.Background(), loaded.Version, updated)
	require.NoError(t, err)
	require.Equal(t, uint64(loaded.Version+1), swapped.Version)

	time.Sleep(2 * time.Millisecond)
	_, err = store.Get(context.Background(), key)
	require.NoError(t, err)
}
