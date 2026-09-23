package gc

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zhenzou/executors"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channelprobe"
	"github.com/looplj/axonhub/internal/ent/datastorage"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestWorker_getBatchSize(t *testing.T) {
	worker := &Worker{
		Ent:    nil,
		Config: Config{CRON: "0 0 * * *"},
	}

	// Test default batch size
	batchSize := worker.getBatchSize()
	if batchSize != defaultBatchSize {
		t.Errorf("Expected batch size %d, got %d", defaultBatchSize, batchSize)
	}

	// Test with overridden batch size
	originalBatchSize := defaultBatchSize
	defaultBatchSize = 20

	defer func() { defaultBatchSize = originalBatchSize }()

	batchSize = worker.getBatchSize()
	if batchSize != 20 {
		t.Errorf("Expected batch size 20, got %d", batchSize)
	}
}

func TestWorker_cleanupRequestExternalStorageDeletesFsArtifacts(t *testing.T) {
	worker, ctx, dataStorage, baseDir := setupWorkerWithFSStorage(t)

	req := &ent.Request{
		ID:            101,
		ProjectID:     202,
		DataStorageID: dataStorage.ID,
	}

	fileKeys := []string{
		biz.GenerateRequestBodyKey(req.ProjectID, req.ID),
		biz.GenerateResponseBodyKey(req.ProjectID, req.ID),
		biz.GenerateResponseChunksKey(req.ProjectID, req.ID),
	}

	dirKeys := []string{
		biz.GenerateRequestExecutionsDirKey(req.ProjectID, req.ID),
		biz.GenerateRequestDirKey(req.ProjectID, req.ID),
	}

	for _, key := range fileKeys {
		createFileForKey(t, baseDir, key)
	}

	for _, key := range dirKeys {
		createDirForKey(t, baseDir, key)
	}

	worker.cleanupRequestExternalStorage(ctx, req, make(map[int]*ent.DataStorage))

	for _, key := range append(fileKeys, dirKeys...) {
		assertRemoved(t, baseDir, key)
	}
}

func TestWorker_cleanupExecutionExternalStorageDeletesFsArtifacts(t *testing.T) {
	worker, ctx, dataStorage, baseDir := setupWorkerWithFSStorage(t)

	req := &ent.Request{
		ID:            303,
		ProjectID:     404,
		DataStorageID: dataStorage.ID,
	}

	exec := &ent.RequestExecution{
		ID:            505,
		RequestID:     req.ID,
		ProjectID:     req.ProjectID,
		DataStorageID: dataStorage.ID,
	}

	fileKeys := []string{
		biz.GenerateExecutionRequestBodyKey(exec.ProjectID, exec.RequestID, exec.ID),
		biz.GenerateExecutionResponseBodyKey(exec.ProjectID, exec.RequestID, exec.ID),
		biz.GenerateExecutionResponseChunksKey(exec.ProjectID, exec.RequestID, exec.ID),
	}

	dirKeys := []string{
		biz.GenerateExecutionRequestDirKey(exec.ProjectID, exec.RequestID, exec.ID),
	}

	for _, key := range fileKeys {
		createFileForKey(t, baseDir, key)
	}

	for _, key := range dirKeys {
		createDirForKey(t, baseDir, key)
	}

	worker.cleanupExecutionExternalStorage(ctx, exec, make(map[int]*ent.DataStorage))

	for _, key := range append(fileKeys, dirKeys...) {
		assertRemoved(t, baseDir, key)
	}
}

func TestHasRealDirectories(t *testing.T) {
	cases := []struct {
		typ  datastorage.Type
		want bool
	}{
		{datastorage.TypeFs, true},
		{datastorage.TypeWebdav, true},
		{datastorage.TypeS3, false},
		{datastorage.TypeGcs, false},
		{datastorage.TypeDatabase, false},
	}

	for _, c := range cases {
		if got := hasRealDirectories(c.typ); got != c.want {
			t.Errorf("hasRealDirectories(%s) = %v, want %v", c.typ, got, c.want)
		}
	}
}

func setupWorkerWithFSStorage(t *testing.T) (*Worker, context.Context, *ent.DataStorage, string) {
	t.Helper()

	cacheConfig := xcache.Config{
		Mode: xcache.ModeMemory,
		Memory: xcache.MemoryConfig{
			Expiration:      5 * time.Minute,
			CleanupInterval: 10 * time.Minute,
		},
	}

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	executor := executors.NewPoolScheduleExecutor(executors.WithMaxConcurrent(1))

	t.Cleanup(func() {
		_ = executor.Shutdown(context.Background())

		client.Close()
	})

	systemService := biz.NewSystemService(biz.SystemServiceParams{CacheConfig: cacheConfig})
	dataStorageService := biz.NewDataStorageService(biz.DataStorageServiceParams{
		SystemService: systemService,
		CacheConfig:   cacheConfig,
		Client:        client,
	})

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	dir := t.TempDir()
	dirCopy := dir
	settings := &objects.DataStorageSettings{Directory: &dirCopy}

	_, err := client.DataStorage.Create().
		SetName("primary").
		SetDescription("primary database").
		SetPrimary(true).
		SetType(datastorage.TypeDatabase).
		SetSettings(&objects.DataStorageSettings{}).
		SetStatus(datastorage.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	dataStorage, err := client.DataStorage.Create().
		SetName("fs-storage").
		SetDescription("test fs storage").
		SetPrimary(false).
		SetType(datastorage.TypeFs).
		SetSettings(settings).
		SetStatus(datastorage.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	worker := &Worker{
		SystemService:      systemService,
		DataStorageService: dataStorageService,
		Ent:                client,
	}

	return worker, ctx, dataStorage, dir
}

func createFileForKey(t *testing.T, baseDir, key string) {
	t.Helper()

	path := pathForKey(baseDir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("test"), 0o644))
}

func createDirForKey(t *testing.T, baseDir, key string) {
	t.Helper()

	path := pathForKey(baseDir, key)
	require.NoError(t, os.MkdirAll(path, 0o755))
}

func assertRemoved(t *testing.T, baseDir, key string) {
	t.Helper()

	path := pathForKey(baseDir, key)
	_, err := os.Stat(path)
	require.ErrorIs(t, err, fs.ErrNotExist, "expected %s to be removed", key)
}

func pathForKey(baseDir, key string) string {
	rel := strings.TrimPrefix(key, "/")
	return filepath.Join(baseDir, filepath.FromSlash(rel))
}

func TestWorker_deleteInBatches(t *testing.T) {
	// Test that the deleteInBatches method works correctly
	// This test verifies the loop logic without needing a real database
	worker := &Worker{
		Ent:    nil,
		Config: Config{CRON: "0 0 * * *"},
	}

	// Simulate batch deletion - delete 3 times, with decreasing counts
	callCount := 0
	deleteFunc := func() (int, error) {
		callCount++
		if callCount == 1 {
			return 30, nil
		} else if callCount == 2 {
			return 15, nil
		} else {
			return 0, nil
		}
	}

	deleted, err := worker.deleteInBatches(context.Background(), deleteFunc)
	if err != nil {
		t.Fatalf("deleteInBatches failed: %v", err)
	}

	// Verify total deleted
	if deleted != 45 {
		t.Errorf("Expected to delete 45 records total, got %d", deleted)
	}

	// Verify it stopped after third call (when 0 was returned)
	if callCount != 3 {
		t.Errorf("Expected 3 delete calls, got %d", callCount)
	}
}

func TestWorker_cleanupWithZeroDays(t *testing.T) {
	worker := &Worker{
		Ent:    nil,
		Config: Config{CRON: "0 0 * * *"},
	}

	ctx := context.Background()

	// Test with 0 days - should not error
	err := worker.cleanupRequests(ctx, 0, false)
	if err != nil {
		t.Fatalf("cleanupRequests with 0 days failed: %v", err)
	}

	// Test with negative days - should not error
	err = worker.cleanupUsageLogs(ctx, -1, false)
	if err != nil {
		t.Fatalf("cleanupUsageLogs with negative days failed: %v", err)
	}
}

func TestWorker_cleanupSQLiteRequestDetailsWhenDatabaseReachesThreshold(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:sqlite-details?mode=memory&_fk=1")
	t.Cleanup(func() { client.Close() })

	ctx := authz.WithTestBypass(context.Background())
	ctx = ent.NewContext(ctx, client)
	project := client.Project.Create().SetName("sqlite-details").SaveX(ctx)
	baseTime := time.Now().Add(-32 * time.Minute)

	for i := 0; i < 32; i++ {
		req := client.Request.Create().
			SetProjectID(project.ID).
			SetModelID("model").
			SetStatus(request.StatusCompleted).
			SetCreatedAt(baseTime.Add(time.Duration(i) * time.Minute)).
			SetRequestHeaders(objects.JSONRawMessage(`{"authorization":"secret"}`)).
			SetRequestBody(objects.JSONRawMessage(`{"request":"detail"}`)).
			SetResponseBody(objects.JSONRawMessage(`{"response":"detail"}`)).
			SetResponseChunks([]objects.JSONRawMessage{objects.JSONRawMessage(`{"chunk":"detail"}`)}).
			SaveX(ctx)
		client.RequestExecution.Create().
			SetRequestID(req.ID).
			SetProjectID(project.ID).
			SetModelID("model").
			SetStatus(requestexecution.StatusCompleted).
			SetCreatedAt(baseTime.Add(time.Duration(i) * time.Minute)).
			SetRequestBody(objects.JSONRawMessage(`{"request":"upstream-detail"}`)).
			SetResponseBody(objects.JSONRawMessage(`{"response":"upstream-detail"}`)).
			SetResponseChunks([]objects.JSONRawMessage{objects.JSONRawMessage(`{"chunk":"upstream-detail"}`)}).
			SetRequestHeaders(objects.JSONRawMessage(`{"authorization":"secret"}`)).
			SetResponseHeaders(objects.JSONRawMessage(`{"x-request-id":"secret"}`)).
			SaveX(ctx)
	}

	worker := &Worker{
		Ent:    client,
		Config: Config{CRON: "0 0 * * *"},
		sqliteDatabaseSize: func(context.Context) (int64, error) {
			return sqliteRequestDetailsSizeThreshold, nil
		},
	}

	require.NoError(t, worker.cleanupSQLiteRequestDetailsIfNeeded(ctx))

	requests := client.Request.Query().Order(ent.Desc(request.FieldCreatedAt)).AllX(ctx)
	require.Len(t, requests, 32)
	for i, req := range requests {
		if i < sqliteRequestDetailsKeepCount {
			require.JSONEq(t, `{"request":"detail"}`, string(req.RequestBody))
			require.NotEmpty(t, req.ResponseBody)
			require.NotEmpty(t, req.ResponseChunks)
			require.NotEmpty(t, req.RequestHeaders)
			continue
		}
		require.JSONEq(t, `{}`, string(req.RequestBody))
		require.Empty(t, req.ResponseBody)
		require.Empty(t, req.ResponseChunks)
		require.Empty(t, req.RequestHeaders)
	}

	executions := client.RequestExecution.Query().AllX(ctx)
	require.Len(t, executions, 32)
	for _, execution := range executions {
		req := client.Request.GetX(ctx, execution.RequestID)
		if req.CreatedAt.After(baseTime.Add(1 * time.Minute)) {
			require.JSONEq(t, `{"request":"upstream-detail"}`, string(execution.RequestBody))
			require.NotEmpty(t, execution.ResponseBody)
			require.NotEmpty(t, execution.ResponseChunks)
			require.NotEmpty(t, execution.RequestHeaders)
			require.NotEmpty(t, execution.ResponseHeaders)
			continue
		}
		require.JSONEq(t, `{}`, string(execution.RequestBody))
		require.Empty(t, execution.ResponseBody)
		require.Empty(t, execution.ResponseChunks)
		require.Empty(t, execution.RequestHeaders)
		require.Empty(t, execution.ResponseHeaders)
	}
}

func TestWorker_cleanupSQLiteRequestDetailsSkipsBelowThreshold(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:sqlite-details-below?mode=memory&_fk=1")
	t.Cleanup(func() { client.Close() })

	ctx := authz.WithTestBypass(context.Background())
	ctx = ent.NewContext(ctx, client)
	project := client.Project.Create().SetName("sqlite-details-below").SaveX(ctx)
	for range sqliteRequestDetailsKeepCount + 1 {
		client.Request.Create().
			SetProjectID(project.ID).
			SetModelID("model").
			SetStatus(request.StatusCompleted).
			SetRequestBody(objects.JSONRawMessage(`{"request":"detail"}`)).
			SetResponseBody(objects.JSONRawMessage(`{"response":"detail"}`)).
			SaveX(ctx)
	}

	worker := &Worker{
		Ent:    client,
		Config: Config{CRON: "0 0 * * *"},
		sqliteDatabaseSize: func(context.Context) (int64, error) {
			return sqliteRequestDetailsSizeThreshold - 1, nil
		},
	}

	require.NoError(t, worker.cleanupSQLiteRequestDetailsIfNeeded(ctx))
	for _, req := range client.Request.Query().AllX(ctx) {
		require.JSONEq(t, `{"request":"detail"}`, string(req.RequestBody))
		require.NotEmpty(t, req.ResponseBody)
	}
}

func TestWorker_cleanupSQLiteRequestDetailsUsesCheckInterval(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:sqlite-details-interval?mode=memory&_fk=1")
	t.Cleanup(func() { client.Close() })

	ctx := authz.WithTestBypass(context.Background())
	ctx = ent.NewContext(ctx, client)
	readCount := 0
	worker := &Worker{
		Ent: client,
		Config: Config{
			CRON:                              "0 0 * * *",
			SQLiteRequestDetailsCheckInterval: time.Hour,
		},
		sqliteDatabaseSize: func(context.Context) (int64, error) {
			readCount++
			return 0, nil
		},
	}

	require.NoError(t, worker.cleanupSQLiteRequestDetailsIfNeeded(ctx))
	require.NoError(t, worker.cleanupSQLiteRequestDetailsIfNeeded(ctx))
	require.Equal(t, 1, readCount)
}

func TestWorkerClearAllRequestRecords(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	t.Cleanup(func() { client.Close() })

	ctx := authz.WithTestBypass(context.Background())
	ctx = ent.NewContext(ctx, client)
	project := client.Project.Create().SetName("clear-all-requests").SaveX(ctx)

	for range 2 {
		req := client.Request.Create().
			SetProjectID(project.ID).
			SetModelID("test-model").
			SetStatus(request.StatusCompleted).
			SetRequestBody(objects.JSONRawMessage(`{}`)).
			SaveX(ctx)
		client.RequestExecution.Create().
			SetRequestID(req.ID).
			SetProjectID(project.ID).
			SetModelID("test-model").
			SetRequestBody(objects.JSONRawMessage(`{}`)).
			SetStatus(requestexecution.StatusCompleted).
			SaveX(ctx)
		client.UsageLog.Create().
			SetRequestID(req.ID).
			SetProjectID(project.ID).
			SetModelID("test-model").
			SetSource(usagelog.SourceAPI).
			SetFormat("openai/chat_completions").
			SaveX(ctx)
	}

	worker := &Worker{Ent: client, Config: Config{CRON: "0 0 * * *"}}
	require.NoError(t, worker.ClearAllRequestRecords(ctx))
	require.Zero(t, client.Request.Query().CountX(ctx))
	require.Zero(t, client.RequestExecution.Query().CountX(ctx))
	require.Zero(t, client.UsageLog.Query().CountX(ctx))
}

func TestWorker_cleanupChannelProbesDeletesInBatches(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	t.Cleanup(func() {
		client.Close()
	})

	ctx := authz.WithTestBypass(context.Background())
	ctx = ent.NewContext(ctx, client)

	originalBatchSize := defaultBatchSize
	defaultBatchSize = 2
	t.Cleanup(func() {
		defaultBatchSize = originalBatchSize
	})

	worker := &Worker{Ent: client, Config: Config{CRON: "0 0 * * *"}}
	oldTimestamp := time.Now().AddDate(0, 0, -5).Unix()
	recentTimestamp := time.Now().Unix()

	for range 5 {
		_, err := client.ChannelProbe.Create().
			SetChannelID(1).
			SetTotalRequestCount(1).
			SetSuccessRequestCount(1).
			SetTimestamp(oldTimestamp).
			Save(ctx)
		require.NoError(t, err)
	}

	for range 2 {
		_, err := client.ChannelProbe.Create().
			SetChannelID(1).
			SetTotalRequestCount(1).
			SetSuccessRequestCount(1).
			SetTimestamp(recentTimestamp).
			Save(ctx)
		require.NoError(t, err)
	}

	require.NoError(t, worker.cleanupChannelProbes(ctx, 3, false))

	oldCount, err := client.ChannelProbe.Query().
		Where(channelprobe.TimestampLT(time.Now().AddDate(0, 0, -3).Unix())).
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, oldCount)

	totalCount, err := client.ChannelProbe.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, totalCount)
}
