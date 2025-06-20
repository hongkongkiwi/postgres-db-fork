package fork_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/fork"
	"github.com/hongkongkiwi/postgres-db-fork/internal/testutil"
	"github.com/stretchr/testify/require"
)

// BenchmarkForkSameServer benchmarks the performance of same-server forking
func BenchmarkForkSameServer(b *testing.B) {
	env, cleanup := testutil.SetupTestEnvironment(b)
	if env == nil {
		b.Skip("Skipping benchmark, test environment not available")
		return
	}
	defer cleanup()

	sourceDB := "bench_same_server_source"
	env.CreateTestDatabase(b, sourceDB)
	env.CreateTestTable(b, sourceDB, "bench_data", 10000) // 10k rows

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer() // Don't time the setup part of the loop
		targetDB := fmt.Sprintf("bench_same_server_target_%d", i)
		cfg := &config.ForkConfig{
			Source:         *env.GetDatabaseConfig(sourceDB),
			Destination:    *env.GetDatabaseConfig("postgres"), // Connect to admin db
			TargetDatabase: targetDB,
			DropIfExists:   true,
			Timeout:        5 * time.Minute,
		}
		forker := fork.NewForker(cfg)
		b.StartTimer() // Time only the Fork operation

		err := forker.Fork(context.Background())
		require.NoError(b, err)
	}
}

// BenchmarkForkCrossServer benchmarks the performance of cross-server forking
func BenchmarkForkCrossServer(b *testing.B) {
	// This benchmark requires two separate database instances.
	// We will simulate this by using two different database configurations.
	// In a real CI environment, you would spin up two Docker containers.
	b.Skip("Skipping cross-server benchmark, requires two database instances setup")
}
