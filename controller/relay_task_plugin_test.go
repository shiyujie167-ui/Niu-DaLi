package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type taskSubmissionTestBilling struct {
	events     *[]string
	settleErr  error
	reserveErr error
	onSettle   func()
	refunds    int
}

func (b *taskSubmissionTestBilling) Settle(int) error {
	*b.events = append(*b.events, "settle")
	if b.onSettle != nil {
		b.onSettle()
	}
	return b.settleErr
}

func (b *taskSubmissionTestBilling) Refund(*gin.Context) {
	*b.events = append(*b.events, "refund")
	b.refunds++
}

func (b *taskSubmissionTestBilling) NeedsRefund() bool        { return b.refunds == 0 }
func (b *taskSubmissionTestBilling) GetPreConsumedQuota() int { return 0 }
func (b *taskSubmissionTestBilling) Reserve(int) error {
	*b.events = append(*b.events, "reserve")
	return b.reserveErr
}

func TestPresentTaskSubmissionUsesNativePresenterAfterPersistence(t *testing.T) {
	plugin, err := pluginruntime.CompilePlugin(`
export const meta = {apiVersion:1,key:"presenter-test",name:"Presenter",version:"1.0.0",author:{name:"Test"},models:["model"],fetchMode:"per_task",routes:[{method:"POST",path:"/vendor/jobs",type:"submit",decode:"decode",render:"created"}]};
export const native = {decode:function(ctx){return {kind:"submit",model:"model",requestBody:ctx.body.value};},created:function(ctx,task){return {data:{task_id:task.task_id},upstream:task.data};}};
export function buildSubmitRequest(){return {}} export function parseSubmitResponse(){return {taskId:"upstream"}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {status:"SUCCESS"}}
`, pluginruntime.Options{})
	require.NoError(t, err)
	priceData := types.PriceData{}
	priceData.AddOtherRatio("seconds", 5)
	task := &model.Task{TaskID: "task_public", SubmitTime: 123}
	task.SetData(map[string]any{"task_id": "upstream_private"})
	outcome := &taskSubmissionOutcome{
		Result:    &relay.TaskSubmitResult{},
		Task:      task,
		RelayInfo: &relaycommon.RelayInfo{PriceData: priceData},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{"model":"model"}`))
	c.Set(pluginruntime.ContextKeyPinnedRoute, pluginruntime.PinnedRoute{Plugin: plugin, Route: plugin.Meta.Routes[0]})
	c.Set(pluginruntime.ContextKeyRouteRequest, pluginruntime.RouteRequestContext{Path: "/vendor/jobs", Method: http.MethodPost, Body: map[string]any{"kind": "json", "value": map[string]any{"model": "model"}}})

	presentTaskSubmission(c, outcome)

	assert.JSONEq(t, `{
		"data":{"task_id":"task_public"},
		"upstream":{"task_id":"upstream_private"}
	}`, recorder.Body.String())
	assert.JSONEq(t, `{"seconds":5}`, recorder.Header().Get("X-New-Api-Other-Ratios"))
}

func TestPresentTaskSubmissionFallbackUsesPersistedPublicID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	outcome := &taskSubmissionOutcome{
		Result:    &relay.TaskSubmitResult{},
		Task:      &model.Task{TaskID: "task_persisted", SubmitTime: 456},
		RelayInfo: &relaycommon.RelayInfo{OriginModelName: "video-model"},
	}

	presentTaskSubmission(c, outcome)

	assert.JSONEq(t, `{
		"id":"task_persisted",
		"task_id":"task_persisted",
		"status":"queued",
		"model":"video-model",
		"created_at":456
	}`, recorder.Body.String())
}

func TestPresentTaskSubmissionUsesHostOpenAIVideoCreateReceipt(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Protocol:  "openai_video",
		Operation: pluginruntime.HostProtocolOperation{Name: "create"},
	})
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		CreatedAt:  456,
		Properties: model.Properties{OriginModelName: "video-model"},
	}
	outcome := &taskSubmissionOutcome{Result: &relay.TaskSubmitResult{}, Task: task, RelayInfo: &relaycommon.RelayInfo{}}

	presentTaskSubmission(c, outcome)

	assert.JSONEq(t, `{"id":"task_public","object":"video","model":"video-model","status":"queued","progress":0,"created_at":456}`, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "task_id")
}

func TestExecuteTaskSubmissionRefundsWhenInsertFails(t *testing.T) {
	events := make([]string, 0, 3)
	database := setupTaskSubmissionDatabase(t, false, &events)
	_ = database
	billing := &taskSubmissionTestBilling{events: &events}
	c := taskSubmissionTestContext()
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream_private",
			Platform:       constant.TaskPlatform("plugin"),
		}, nil
	})

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "task_insert_failed", taskErr.Code)
	assert.Equal(t, []string{"reserve", "insert", "refund"}, events)
	assert.Equal(t, 1, billing.refunds)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionSettlementFailureStaysDurableAndWritesNothing(t *testing.T) {
	events := make([]string, 0, 3)
	database := setupTaskSubmissionDatabase(t, true, &events)
	billing := &taskSubmissionTestBilling{events: &events, settleErr: errors.New("settlement failed")}
	c := taskSubmissionTestContext()
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream_private",
			Platform:       constant.TaskPlatform("plugin"),
		}, nil
	})

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "task_billing_settlement_failed", taskErr.Code)
	assert.Equal(t, []string{"reserve", "insert", "settle"}, events)
	assert.Zero(t, billing.refunds)
	var count int64
	require.NoError(t, database.Model(&model.Task{}).Where("task_id = ?", "task_public").Count(&count).Error)
	assert.Equal(t, int64(1), count)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionPersistsPinnedPluginProvenance(t *testing.T) {
	events := make([]string, 0, 3)
	database := setupTaskSubmissionDatabase(t, true, &events)
	previousLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })

	c := taskSubmissionTestContext()
	c.Set(common.RequestIdKey, "request-public")
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{
		Generation: &pluginruntime.RoutingGeneration{Number: 42},
		Plugin: &pluginruntime.LoadedPlugin{Meta: pluginruntime.Meta{
			Key:        "document-parser",
			Name:       "Document Parser",
			Version:    "1.2.3",
			APIVersion: 1,
			Author: pluginruntime.AuthorMeta{
				Name: "Community Author",
				URL:  "https://plugins.example/author",
			},
		}},
	})
	billing := &taskSubmissionTestBilling{events: &events}
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-private",
			Platform:       constant.TaskPlatform("document-parser"),
		}, nil
	})

	require.Nil(t, taskErr)
	require.NotNil(t, outcome)
	require.NotNil(t, outcome.Task.PrivateData.Execution)
	require.NotNil(t, outcome.Task.PrivateData.Execution.TaskPlugin)
	assert.Equal(t, "request-public", outcome.Task.PrivateData.Execution.RequestID)
	assert.Equal(t, "/plugin/submit", outcome.Task.PrivateData.Execution.RequestPath)
	assert.Equal(t, "1.2.3", outcome.Task.PrivateData.Execution.TaskPlugin.Version)
	assert.Equal(t, uint64(42), outcome.Task.PrivateData.Execution.TaskPlugin.Generation)
	require.NotNil(t, outcome.Task.PrivateData.Execution.TaskPlugin.Author)
	assert.Equal(t, "Community Author", outcome.Task.PrivateData.Execution.TaskPlugin.Author.Name)
	assert.Equal(t, "https://plugins.example/author", outcome.Task.PrivateData.Execution.TaskPlugin.Author.URL)

	var stored model.Task
	require.NoError(t, database.Where("task_id = ?", "task_public").First(&stored).Error)
	require.NotNil(t, stored.PrivateData.Execution)
	require.NotNil(t, stored.PrivateData.Execution.TaskPlugin)
	assert.Equal(t, "document-parser", stored.PrivateData.Execution.TaskPlugin.Key)
	require.NotNil(t, stored.PrivateData.Execution.TaskPlugin.Author)
	assert.Equal(t, "Community Author", stored.PrivateData.Execution.TaskPlugin.Author.Name)
	assert.Equal(t, "upstream-private", stored.PrivateData.UpstreamTaskID)
}

// A submit route declaring retainResult: false persists the task row for
// billing but never writes the upstream snapshot for an immediate terminal
// result, while the in-memory task still carries it for the presenter. An
// asynchronous result on the same route is retained because polling and
// retrieval depend on it.
func TestExecuteTaskSubmissionHonorsRouteRetainResult(t *testing.T) {
	retainFalse := false
	for _, tc := range []struct {
		name          string
		immediate     *relaycommon.TaskInfo
		wantDiscarded bool
	}{
		{"immediate success is discarded", &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Progress: "100%"}, true},
		{"immediate failure is discarded", &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "rejected"}, true},
		{"asynchronous result is retained", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := make([]string, 0, 3)
			database, _ := openTaskDialectDatabase(t, &model.Task{}, &model.User{}, &model.Channel{})
			previousDB := model.DB
			model.DB = database
			t.Cleanup(func() { model.DB = previousDB })
			previousLogConsumeEnabled := common.LogConsumeEnabled
			common.LogConsumeEnabled = false
			t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })

			c := taskSubmissionTestContext()
			c.Set(pluginruntime.ContextKeyPinnedRoute, pluginruntime.PinnedRoute{
				Plugin: &pluginruntime.LoadedPlugin{Meta: pluginruntime.Meta{Key: "sync-images"}},
				Route:  pluginruntime.Route{Method: http.MethodPost, Path: "/sync/images", Type: pluginruntime.RouteTypeSubmit, RetainResult: &retainFalse},
			})
			info := taskSubmissionRelayInfo(&taskSubmissionTestBilling{events: &events})
			upstream := []byte(`{"data":[{"url":"https://cdn.example/a.png"}]}`)

			outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				return &relay.TaskSubmitResult{
					UpstreamTaskID: "task_public",
					Platform:       constant.TaskPlatform("sync-images"),
					TaskData:       upstream,
					Immediate:      tc.immediate,
				}, nil
			})
			require.Nil(t, taskErr)
			require.NotNil(t, outcome)
			assert.JSONEq(t, string(upstream), string(outcome.Task.Data), "presenter keeps the in-memory snapshot")
			assert.Equal(t, tc.wantDiscarded, outcome.Task.PrivateData.ResultDiscarded)
			assert.Equal(t, !tc.wantDiscarded, outcome.Task.ResultRetrievable())

			var stored model.Task
			require.NoError(t, database.Where("task_id = ?", "task_public").First(&stored).Error)
			assert.Equal(t, tc.wantDiscarded, stored.PrivateData.ResultDiscarded)
			var nullCount int64
			require.NoError(t, database.Model(&model.Task{}).Where("task_id = ? AND data IS NULL", "task_public").Count(&nullCount).Error)
			if tc.wantDiscarded {
				assert.Equal(t, int64(1), nullCount, "snapshot column must stay NULL")
				artifacts, err := projectTaskArtifacts(&stored)
				require.NoError(t, err)
				assert.Empty(t, artifacts)
			} else {
				assert.Equal(t, int64(0), nullCount)
				assert.JSONEq(t, string(upstream), string(stored.Data))
			}
			listed := model.TaskGetAllUserTask(1, 0, 10, model.SyncTaskQueryParams{})
			require.Len(t, listed, 1)
			assert.Empty(t, listed[0].Data, "task lists never select the snapshot column")
		})
	}
}

func TestExecuteTaskSubmissionRefundsCancellationBeforeDurableBarrier(t *testing.T) {
	events := make([]string, 0, 2)
	setupTaskSubmissionDatabase(t, true, &events)
	billing := &taskSubmissionTestBilling{events: &events}
	c := taskSubmissionTestContext()
	requestContext, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(requestContext)
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		cancel()
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream_private",
			Platform:       constant.TaskPlatform("plugin"),
		}, nil
	})

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "request_cancelled", taskErr.Code)
	assert.Equal(t, []string{"refund"}, events)
	assert.Equal(t, 1, billing.refunds)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionDisconnectBeforeUpstreamAcceptanceSkipsSubmitAndRefunds(t *testing.T) {
	events := make([]string, 0, 1)
	setupTaskSubmissionDatabase(t, true, &events)
	billing := &taskSubmissionTestBilling{events: &events}
	c := taskSubmissionTestContext()
	requestContext, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(requestContext)
	info := taskSubmissionRelayInfo(billing)
	submitted := false

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		submitted = true
		return nil, nil
	})

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "request_cancelled", taskErr.Code)
	assert.False(t, submitted)
	assert.Equal(t, []string{"refund"}, events)
	assert.Equal(t, 1, billing.refunds)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionCallerCancellationDuringSubmitRefundsBeforeDurableBarrier(t *testing.T) {
	events := make([]string, 0, 1)
	setupTaskSubmissionDatabase(t, true, &events)
	billing := &taskSubmissionTestBilling{events: &events}
	c := taskSubmissionTestContext()
	requestContext, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(requestContext)
	info := taskSubmissionRelayInfo(billing)
	submitStarted := make(chan struct{})
	done := make(chan struct{})
	var outcome *taskSubmissionOutcome
	var taskErr *dto.TaskError

	go func() {
		defer close(done)
		outcome, taskErr = executeTaskSubmissionWith(c, info, func(c *gin.Context, _ *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
			close(submitStarted)
			<-c.Request.Context().Done()
			return nil, service.TaskErrorWrapperLocal(c.Request.Context().Err(), "do_request_failed", http.StatusInternalServerError)
		})
	}()
	select {
	case <-submitStarted:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "submission did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "submission did not stop after disconnect")
	}

	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "request_cancelled", taskErr.Code)
	assert.Equal(t, []string{"refund"}, events)
	assert.Equal(t, 1, billing.refunds)
	assert.False(t, c.Writer.Written())
}

func TestExecuteTaskSubmissionDisconnectAfterDurableInsertDoesNotRefund(t *testing.T) {
	events := make([]string, 0, 3)
	database := setupTaskSubmissionDatabase(t, true, &events)
	previousLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })
	c := taskSubmissionTestContext()
	requestContext, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(requestContext)
	billing := &taskSubmissionTestBilling{
		events:   &events,
		onSettle: cancel,
	}
	info := taskSubmissionRelayInfo(billing)

	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream_private",
			Platform:       constant.TaskPlatform("plugin"),
		}, nil
	})

	require.Nil(t, taskErr)
	require.NotNil(t, outcome)
	assert.Equal(t, "task_public", outcome.Task.TaskID)
	assert.Equal(t, []string{"reserve", "insert", "settle"}, events)
	assert.Zero(t, billing.refunds)
	var count int64
	require.NoError(t, database.Model(&model.Task{}).Where("task_id = ?", "task_public").Count(&count).Error)
	assert.Equal(t, int64(1), count)
	assert.False(t, c.Writer.Written())
}

// setupTaskSubmissionDatabase opens the dialect selected by
// TEST_TASK_DB_DIALECT (SQLite in memory by default) and records every task
// INSERT in events so tests can assert the reserve → insert → settle order.
// Without migrate the task table does not exist and inserts fail.
func setupTaskSubmissionDatabase(t *testing.T, migrate bool, events *[]string) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	var models []any
	if migrate {
		models = append(models, &model.Task{})
	}
	database, _ := openTaskDialectDatabase(t, models...)
	require.NoError(t, database.Callback().Create().Before("gorm:create").Register("test:task-submit-order", func(*gorm.DB) {
		*events = append(*events, "insert")
	}))
	model.DB = database
	t.Cleanup(func() { model.DB = previousDB })
	return database
}

func taskSubmissionTestContext() *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/plugin/submit", strings.NewReader(`{}`))
	return c
}

func taskSubmissionRelayInfo(billing relaycommon.BillingSettler) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          1,
		UsingGroup:      "default",
		OriginModelName: "plugin-model",
		Billing:         billing,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID:  "task_public",
			LockedChannel: &model.Channel{Id: 1, Type: constant.ChannelTypeTaskPlugin, Name: "plugin"},
		},
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1, ChannelType: constant.ChannelTypeTaskPlugin},
	}
}

// Uses the real submit adaptor, expression evaluator, BillingSession, task row
// and consume log. Set TEST_TASK_DB_DIALECT plus TEST_MYSQL_DSN or
// TEST_POSTGRES_DSN to exercise the same contract on an external test database.
// Unique table prefixes keep the fixture isolated from all existing tables.
// openTaskDialectDatabase opens the engine selected by TEST_TASK_DB_DIALECT
// (default SQLite in memory; MySQL and PostgreSQL through TEST_MYSQL_DSN and
// TEST_POSTGRES_DSN) with a unique table prefix, migrates the given models and
// drops them on cleanup. It logs the engine version so database verification
// runs leave a record.
func openTaskDialectDatabase(t *testing.T, models ...any) (*gorm.DB, common.DatabaseType) {
	t.Helper()
	dialect := common.DatabaseType(os.Getenv("TEST_TASK_DB_DIALECT"))
	var driver gorm.Dialector
	switch dialect {
	case "", common.DatabaseTypeSQLite:
		dialect = common.DatabaseTypeSQLite
		driver = sqlite.Open(":memory:")
	case common.DatabaseTypeMySQL:
		require.NotEmpty(t, os.Getenv("TEST_MYSQL_DSN"))
		driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
	case common.DatabaseTypePostgreSQL:
		require.NotEmpty(t, os.Getenv("TEST_POSTGRES_DSN"))
		driver = postgres.New(postgres.Config{DSN: os.Getenv("TEST_POSTGRES_DSN"), PreferSimpleProtocol: true})
	default:
		t.Fatalf("unsupported test dialect %q", dialect)
	}
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("tsubmit_%d_", time.Now().UnixNano())}})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(models...))
	t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(models...)) })
	var version string
	if dialect == common.DatabaseTypeSQLite {
		require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
	} else {
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	}
	t.Logf("database: %s %s", dialect, version)
	return db, dialect
}

func TestImmediateTaskSettlementDatabase(t *testing.T) {
	db, dialect := openTaskDialectDatabase(t, &model.User{}, &model.Channel{}, &model.Task{}, &model.Log{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldMemory, oldBatch, oldConsume, oldExport := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(dialect, dialect)
	common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, false, false, true, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
		common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = oldRedis, oldMemory, oldBatch, oldConsume, oldExport
	})

	const expression = `u("units") == 7 ? tier("missing", u("missing") * 1.0) : u("units") == 8 ? tier("invalid", -1.0) : u("units") > 3 ? tier("bulk", u("units") * 0.01) : tier("small", u("units") * 0.01)`
	withTieredBillingConfig(t, map[string]string{"document-model": "tiered_expr"}, map[string]string{"document-model": expression})
	const source = `
export const meta={apiVersion:1,key:"generic-settlement",name:"Generic settlement",version:"1.0.0",author:{name:"Test"},models:["document-model"],fetchMode:"per_task",usageSchema:{units:{type:"number",unit:"count"}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/compile",body:ctx.requestBody};}
export function parseSubmitResponse(ctx,resp){return {taskId:"vendor-job",taskData:resp.body,immediate:{status:resp.body.status,reason:"provider rejected job"}};}
export function extractUsage(){return {units:4};}
export function extractUsageOnComplete(ctx,result,body){return body.usage;}
export function parseTaskResult(){throw new Error("completed submissions must not poll");}
export function buildQueryRequest(){throw new Error("completed submissions must not poll");}
`
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	for index, tc := range []struct {
		name, status string
		actual       any
		count        float64
	}{
		{"partial", "SUCCESS", 2, 2}, {"zero", "SUCCESS", 0, 0}, {"larger", "SUCCESS", 6, 6},
		{"invalid usage", "SUCCESS", -1, 4}, {"expression failure", "SUCCESS", 7, 4}, {"negative result", "SUCCESS", 8, 4}, {"missing usage", "SUCCESS", nil, 4}, {"failed", "FAILURE", 9, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				body := map[string]any{"status": tc.status}
				if tc.actual != nil {
					body["usage"] = map[string]any{"units": tc.actual}
				}
				encoded, err := common.Marshal(body)
				if err != nil {
					panic(err)
				}
				_, _ = w.Write(encoded)
			}))
			defer server.Close()
			initial := int(20 * common.QuotaPerUnit)
			user := model.User{Username: fmt.Sprintf("task_user_%d", index), AffCode: fmt.Sprintf("task_aff_%d", index), Quota: initial}
			require.NoError(t, db.Create(&user).Error)
			ch := model.Channel{Name: "test provider", Type: constant.ChannelTypeTaskPlugin}
			require.NoError(t, db.Create(&ch).Error)
			c := taskSubmissionTestContext()
			c.Set("group", "default")
			c.Set("username", user.Username)
			c.Set("task_request", map[string]any{"model": "document-model"})
			c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "document-model")
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
			common.SetContextKey(c, constant.ContextKeyChannelId, ch.Id)
			common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
			info := taskSubmissionRelayInfo(nil)
			info.UserId = user.Id
			info.OriginModelName = "document-model"
			info.UserGroup = "default"
			info.UsingGroup = "default"
			info.IsPlayground = true
			info.UserSetting.BillingPreference = "wallet_only"
			info.PublicTaskID = model.GenerateTaskID()
			info.LockedChannel = &ch
			outcome, taskErr := executeTaskSubmission(c, info)
			require.Nil(t, taskErr)
			require.NotNil(t, outcome)
			want := common.QuotaRound(tc.count * 0.01 * common.QuotaPerUnit)
			assert.Equal(t, want, outcome.Result.Quota)
			assert.Equal(t, want, info.PriceData.Quota)
			var stored model.Task
			require.NoError(t, db.Where("task_id = ?", info.PublicTaskID).First(&stored).Error)
			assert.Equal(t, want, stored.Quota)
			assert.Equal(t, model.TaskStatus(tc.status), stored.Status)
			assert.Positive(t, stored.FinishTime)
			assert.Equal(t, float64(4), info.TieredBillingSnapshot.EstimatedQuotaBeforeGroup/(0.01*common.QuotaPerUnit))
			if tc.status == "SUCCESS" {
				assert.Equal(t, tc.count, stored.PrivateData.BillingContext.TieredSnapshot.UsageFacts["units"])
			}
			var updated model.User
			require.NoError(t, db.First(&updated, user.Id).Error)
			assert.Equal(t, initial-want, updated.Quota)
			assert.Equal(t, want, updated.UsedQuota)
			var logs []model.Log
			require.NoError(t, db.Where("user_id = ?", user.Id).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, want, logs[0].Quota)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			if tc.status == "SUCCESS" {
				assert.Equal(t, tc.count, other["usage_facts"].(map[string]any)["units"])
			}
			assert.False(t, c.Writer.Written(), "presentation must follow persistence and settlement")
			require.NoError(t, info.Billing.Settle(want))
			info.Billing.Refund(c)
			require.NoError(t, db.First(&updated, user.Id).Error)
			assert.Equal(t, initial-want, updated.Quota, "terminal settlement is idempotent")
		})
	}
}

func TestAcceptedSubmitStreamNeverRetries(t *testing.T) {
	c := taskSubmissionTestContext()
	assert.Equal(t, service.PolicyDecision{Action: "stop", Reason: "task_accepted", Source: "system"}, decideTaskRetry(c, &dto.TaskError{StatusCode: 502, LocalError: true, NoRetry: true}, 3))
}

// Local task rejections carry a message but no cause; the response and the
// decision record must still be produced.
func TestRespondTaskSubmissionErrorWithoutCause(t *testing.T) {
	previousErrorLog := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = false
	t.Cleanup(func() { constant.ErrorLogEnabled = previousErrorLog })
	c := taskSubmissionTestContext()
	taskErr := &dto.TaskError{Code: "get_channel_failed", Message: "no channel", StatusCode: http.StatusServiceUnavailable, LocalError: true}
	require.NotPanics(t, func() { respondTaskSubmissionError(c, taskErr) })
	assert.Equal(t, http.StatusServiceUnavailable, c.Writer.Status())
	events := service.RequestPolicy(c).Events()
	require.Len(t, events, 1)
	assert.Equal(t, service.PolicyDecision{Action: "stop", Reason: "request_failed", Source: "system"}, events[0].Decision)
	assert.Equal(t, http.StatusServiceUnavailable, events[0].Status)
	assert.Equal(t, service.PolicyDecision{Action: "stop", Reason: "local_rejection", Source: "system"}, decideTaskRetry(c, &dto.TaskError{StatusCode: http.StatusForbidden, LocalError: true, Message: "billing"}, 2), "local errors stop once the status rules do not force a retry")
}

func TestExecuteTaskSubmissionRefundsWhenFinalReserveFails(t *testing.T) {
	events := []string{}
	setupTaskSubmissionDatabase(t, true, &events)
	billing := &taskSubmissionTestBilling{events: &events, reserveErr: errors.New("insufficient funds")}
	c := taskSubmissionTestContext()
	info := taskSubmissionRelayInfo(billing)
	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{Platform: "plugin", Quota: 600, Immediate: &relaycommon.TaskInfo{Status: "SUCCESS"}}, nil
	})
	require.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusForbidden, taskErr.StatusCode)
	assert.Equal(t, []string{"reserve", "refund"}, events)
	assert.Equal(t, 1, billing.refunds)
	assert.False(t, c.Writer.Written())
}

// This fixture uses disposable dialect databases and a local fake provider;
// it never reads channel keys, users, or balances from a running deployment.
func videoWorkspaceTestRouter(t *testing.T) (*gorm.DB, *gin.Engine, *atomic.Int32) {
	t.Helper()
	db, dialect := openTaskDialectDatabase(t, &model.User{}, &model.Channel{}, &model.Ability{}, &model.Task{}, &model.Log{}, &model.Token{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldMemory, oldBatch, oldConsume, oldExport := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(dialect, dialect)
	common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, false, false, true, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
		common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = oldRedis, oldMemory, oldBatch, oldConsume, oldExport
	})
	withTieredBillingConfig(t, map[string]string{"sora-2": "tiered_expr"}, map[string]string{"sora-2": `tier("video", u("seconds") * 0.01)`})
	_, err := pluginruntime.DefaultRegistry.Register(`
 export const meta={apiVersion:1,key:"sora",name:"Workspace test provider",version:"1.0.0",author:{name:"Test"},models:["sora-2"],channelTypes:[55,1],protocols:["openai_video"],fetchMode:"per_task",usageSchema:{seconds:{type:"number",unit:"second",description:{en:"Video generation unit price"}}}};
 export const protocols={openai_video:{decodeRequest:function(ctx){const f=ctx.body.fields;return {kind:"submit",model:ctx.model,action:"text_to_video",requestBody:{prompt:f.prompt[0],seconds:Number((f.seconds||[4])[0])}}},render:function(ctx,task){return {id:task.task_id}}}};
 export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/videos",headers:{Authorization:"Bearer "+ctx.apiKey},body:ctx.requestBody};}
 export function parseSubmitResponse(ctx,response){return {taskId:"upstream-"+utils.uuid(),taskData:response.body,immediate:{status:response.body.status,reason:"provider failed",url:"https://cdn.example/video.mp4"}};}
 export function extractUsage(ctx){return {seconds:ctx.requestBody.seconds};}
 export function listArtifacts(){return [{key:"video",type:"video",mimeType:"video/mp4"}];}
 export function buildContentRequest(ctx){return {url:ctx.baseUrl+"/content",method:"GET",headers:{Authorization:"Bearer "+ctx.apiKey}};}
 export function buildQueryRequest(){throw new Error("terminal task must not poll");}
 export function parseTaskResult(){throw new Error("terminal task must not poll");}
 `, pluginruntime.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pluginruntime.DefaultRegistry.Unregister("sora")) })
	calls := &atomic.Int32{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer local-test-channel-key", r.Header.Get("Authorization"))
		if r.URL.Path == "/content" {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("test-video-content"))
			return
		}
		calls.Add(1)
		var request map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &request))
		w.Header().Set("Content-Type", "application/json")
		if request["prompt"] == "fail" {
			_, _ = io.WriteString(w, `{"status":"FAILURE"}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"SUCCESS"}`)
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()
	user := model.User{Id: 7, Username: "workspace-owner", AffCode: "workspace-owner", Status: common.UserStatusEnabled, Role: common.RoleRootUser, Group: "default", Quota: 1_000_000}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.Token{Id: 11, UserId: 7, Key: "unused-workspace-api-token", Name: "unchanged", RemainQuota: 123456}).Error)
	channel := model.Channel{Name: "workspace-local-provider", Type: constant.ChannelTypeSora, Key: "local-test-channel-key", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Models: "sora-2", Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "sora-2", ChannelId: channel.Id, Enabled: true}).Error)
	engine := gin.New()
	engine.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
		c.Set("id", 7)
		c.Set("role", common.RoleRootUser)
		c.Set("username", user.Username)
		c.Set("session_id", "test-session")
		c.Set("auth_version", int64(1))
		c.Set("session_version", int64(1))
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserSetting, kitdto.UserSetting{BillingPreference: "subscription_only"})
		c.Next()
	}, VideoWorkspaceSession)
	engine.GET("/api/video-workspace/models", GetVideoWorkspaceModels)
	engine.GET("/api/video-workspace/tasks", GetVideoWorkspaceTasks)
	engine.GET("/api/video-workspace/tasks/:task_id/artifacts", GetVideoWorkspaceArtifacts)
	engine.GET("/api/video-workspace/tasks/:task_id/artifacts/:artifact_key/content", VideoWorkspaceArtifactContent)
	engine.POST("/api/video-workspace/tasks", middleware.BrowserOriginGuard(), PrepareVideoWorkspaceSubmission, middleware.PinTaskPluginEndpoint(), middleware.PrepareTaskPluginEndpoint(), func(c *gin.Context) {
		require.Nil(t, middleware.SetupContextForSelectedChannel(c, &channel, "sora-2"))
		c.Next()
	}, func(c *gin.Context) { RelayTaskPluginEndpoint(c, RelayTask) })
	return db, engine, calls
}

func TestVideoWorkspaceWalletPersistenceAndOwnership(t *testing.T) {
	db, engine, calls := videoWorkspaceTestRouter(t)
	var taskIDs []string
	for _, prompt := range []string{"a lighthouse", "fail"} {
		request := httptest.NewRequest(http.MethodPost, "http://workspace.example/api/video-workspace/tasks", strings.NewReader(`{"model":"sora-2","prompt":"`+prompt+`","seconds":4}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://workspace.example")
		request.Header.Set("Authorization", "Bearer browser-session-secret")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var receipt struct {
			ID string `json:"id"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &receipt))
		require.NotEmpty(t, receipt.ID)
		taskIDs = append(taskIDs, receipt.ID)
	}
	assert.Equal(t, int32(2), calls.Load())
	var owner model.User
	require.NoError(t, db.First(&owner, 7).Error)
	wantCharge := common.QuotaRound(4 * 0.01 * common.QuotaPerUnit)
	assert.Equal(t, 1_000_000-wantCharge, owner.Quota, "one successful task is charged once; failed completion returns its reservation")
	var token model.Token
	require.NoError(t, db.First(&token, 11).Error)
	assert.Equal(t, 123456, token.RemainQuota)
	var tasks []model.Task
	require.NoError(t, db.Order("id ASC").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	assert.Equal(t, "a lighthouse", tasks[0].Properties.Input)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), tasks[0].Status)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), tasks[1].Status)
	assert.Zero(t, tasks[1].Quota)
	for _, task := range tasks {
		assert.Zero(t, task.PrivateData.TokenId)
		assert.Equal(t, "wallet", task.PrivateData.BillingSource)
	}
	require.NoError(t, db.Create(&model.Task{UserId: 8, TaskID: "other-user-video", Action: constant.TaskActionTextToVideo, Status: model.TaskStatusSuccess}).Error)
	require.NoError(t, db.Create(&model.Task{UserId: 7, TaskID: "owner-image", Action: "image_generation", Status: model.TaskStatusSuccess}).Error)
	for range 2 {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-workspace/tasks?p=1&page_size=10", nil))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var history struct {
			Data struct {
				Total int           `json:"total"`
				Items []dto.TaskDto `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &history))
		assert.Equal(t, 2, history.Data.Total)
		for _, task := range history.Data.Items {
			assert.Equal(t, 7, task.UserId)
			assert.JSONEq(t, "null", string(task.Data))
			assert.Nil(t, task.AdminInfo)
			assert.Zero(t, task.ChannelId)
		}
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-workspace/tasks/other-user-video/artifacts", nil))
	assert.Equal(t, http.StatusNotFound, response.Code, "root role must not grant another user's result through workspace")

	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-workspace/tasks/other-user-video/artifacts/video/content", nil))
	assert.Equal(t, http.StatusNotFound, response.Code, "content must enforce owner scope for administrators")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-workspace/tasks/"+taskIDs[0]+"/artifacts", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "/api/video-workspace/tasks/"+taskIDs[0]+"/artifacts/video/content")
	assert.NotContains(t, response.Body.String(), "access=")
	assert.NotContains(t, response.Body.String(), "cdn.example")
	allowPrivateTaskMediaTest(t)
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/video-workspace/tasks/"+taskIDs[0]+"/artifacts/video/content", nil)
	request.Header.Set("Authorization", "Bearer browser-session-secret")
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "test-video-content", response.Body.String())
	assert.Equal(t, "video/mp4", response.Header().Get("Content-Type"))
	assert.Equal(t, "same-origin", response.Header().Get("Cross-Origin-Resource-Policy"))
	assert.NotContains(t, response.Body.String(), "local-test-channel-key")
	assert.NotContains(t, response.Header().Get("Authorization"), "local-test-channel-key")
	require.NoError(t, db.First(&owner, 7).Error)
	assert.Equal(t, 1_000_000-wantCharge, owner.Quota, "history polling never charges")
	var logs int64
	require.NoError(t, db.Model(&model.Log{}).Where("user_id = ?", 7).Count(&logs).Error)
	assert.Equal(t, int64(2), logs)
}

func TestVideoWorkspaceValidationAndCatalog(t *testing.T) {
	db, engine, calls := videoWorkspaceTestRouter(t)
	for _, tc := range []struct {
		name, body, origin string
		status             int
	}{
		{"foreign origin", `{"model":"sora-2","prompt":"test"}`, "https://foreign.example", 403},
		{"missing origin", `{"model":"sora-2","prompt":"test"}`, "", 403},
		{"missing channel", `{"model":"missing","prompt":"test"}`, "http://workspace.example", 400},
		{"negative duration", `{"model":"sora-2","prompt":"test","seconds":-4}`, "http://workspace.example", 400},
		{"oversized duration", `{"model":"sora-2","prompt":"test","seconds":3601}`, "http://workspace.example", 400},
		{"fractional duration", `{"model":"sora-2","prompt":"test","seconds":4.5}`, "http://workspace.example", 400},
		{"unsupported size", `{"model":"sora-2","prompt":"test","size":"1792x1024"}`, "http://workspace.example", 400},
		{"metadata bypass", `{"model":"sora-2","prompt":"test","metadata":{"duration":9999}}`, "http://workspace.example", 400},
		{"group bypass", `{"model":"sora-2","prompt":"test","group":"vip"}`, "http://workspace.example", 400},
		{"empty prompt", `{"model":"sora-2","prompt":" "}`, "http://workspace.example", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://workspace.example/api/video-workspace/tasks", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			assert.Equal(t, tc.status, response.Code, response.Body.String())
		})
	}
	for _, valid := range []bool{false, true} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "sora-2"))
		require.NoError(t, writer.WriteField("prompt", "uploaded image"))
		part, err := writer.CreateFormFile("input_reference", "reference.png")
		require.NoError(t, err)
		if valid {
			require.NoError(t, png.Encode(part, image.NewRGBA(image.Rect(0, 0, 2, 2))))
		} else {
			_, err = part.Write([]byte("fake image"))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		req := httptest.NewRequest(http.MethodPost, "http://workspace.example/api/video-workspace/tasks", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Origin", "http://workspace.example")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		if valid {
			assert.Equal(t, 200, response.Code, response.Body.String())
		} else {
			assert.Equal(t, 400, response.Code, response.Body.String())
		}
	}
	assert.Equal(t, int32(1), calls.Load(), "only validated input reaches the provider")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-workspace/models", nil))
	require.Equal(t, 200, response.Code, response.Body.String())
	var catalog struct {
		Data struct {
			Models []videoWorkspaceModel `json:"models"`
			Quota  int                   `json:"quota"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &catalog))
	require.Len(t, catalog.Data.Models, 1)
	assert.True(t, catalog.Data.Models[0].SupportsImage)
	require.NoError(t, db.Model(&model.Channel{}).Where("models = ?", "sora-2").Update("status", common.ChannelStatusManuallyDisabled).Error)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-workspace/models", nil))
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &catalog))
	assert.Empty(t, catalog.Data.Models)
}

func TestVideoWorkspaceRequiresLiveSessionIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		userID  int
		session string
		version int64
		pat     bool
		want    int
	}{
		{"anonymous", 0, "", 0, false, 401}, {"api token", 7, "", 0, false, 401}, {"personal access token", 7, "session", 1, true, 401}, {"missing session version", 7, "session", 0, false, 401}, {"browser session", 7, "session", 1, false, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.GET("/", func(c *gin.Context) {
				c.Set("id", tc.userID)
				c.Set("session_id", tc.session)
				c.Set("auth_version", tc.version)
				c.Set("session_version", tc.version)
				c.Set("use_access_token", tc.pat)
			}, VideoWorkspaceSession, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			assert.Equal(t, tc.want, response.Code)
		})
	}
	c := taskSubmissionTestContext()
	c.Set(constant.ContextKeyVideoWorkspace, true)
	assert.Equal(t, service.PolicyDecision{Action: "stop", Reason: "video_workspace_single_attempt", Source: "system"}, decideTaskRetry(c, &dto.TaskError{StatusCode: 503}, 3))
}

func TestVideoWorkspaceRejectsExpiredAndRevokedSessions(t *testing.T) {
	db, _, calls := videoWorkspaceTestRouter(t)
	require.NoError(t, db.AutoMigrate(&model.UserSession{}))
	t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&model.UserSession{})) })
	oldSecret := common.SessionSecret
	common.SessionSecret = "video-workspace-test-session-secret"
	t.Cleanup(func() { common.SessionSecret = oldSecret })
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 7).Update("auth_version", 1).Error)
	session := model.UserSession{SID: "workspace-test-session", UserID: 7, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, RefreshHash: strings.Repeat("a", 64), LoginMethod: "password", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, db.Create(&session).Error)
	access, _, err := service.IssueAccessToken(service.AuthIdentity{UserID: 7, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1})
	require.NoError(t, err)
	engine := gin.New()
	engine.GET("/models", middleware.UserAuth(), VideoWorkspaceSession, GetVideoWorkspaceModels)
	for _, tc := range []struct {
		name    string
		expires int64
		status  string
		want    int
	}{
		{"active", time.Now().Add(time.Hour).Unix(), model.UserSessionStatusActive, 200},
		{"expired", time.Now().Add(-time.Minute).Unix(), model.UserSessionStatusActive, 401},
		{"revoked", time.Now().Add(time.Hour).Unix(), model.UserSessionStatusRevoked, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Model(&model.UserSession{}).Where("sid = ?", session.SID).Updates(map[string]any{"expires_at": tc.expires, "status": tc.status}).Error)
			req := httptest.NewRequest(http.MethodGet, "/models", nil)
			req.Header.Set("Authorization", "Bearer "+access)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			assert.Equal(t, tc.want, response.Code, response.Body.String())
		})
	}
	assert.Zero(t, calls.Load())
}

func TestVideoWorkspaceDoubaoCatalogRequiresTextInputSupport(t *testing.T) {
	db, engine, _ := videoWorkspaceTestRouter(t)
	supported := []string{"doubao-seedance-1-0-pro-250528", "doubao-seedance-1-0-lite-t2v", "doubao-seedance-1-5-pro-251215", "doubao-seedance-2-0-260128", "doubao-seedance-2-0-fast-260128", "doubao-seedance-2-0-mini-260615", "doubao-seedance-2-5-260628"}
	names := append(append([]string{}, supported...), "doubao-seedance-1-0-lite-i2v")
	modes, expressions := map[string]string{}, map[string]string{}
	for _, name := range names {
		require.Len(t, pluginruntime.DefaultRegistry.Generation().LookupEndpointCandidates(http.MethodPost, "/v1/videos", name), 1)
		modes[name], expressions[name] = "tiered_expr", `tier("video", u("tokens") * 0.01)`
	}
	withTieredBillingConfig(t, modes, expressions)
	require.NoError(t, db.Model(&model.Channel{}).Where("models = ?", "sora-2").Update("status", common.ChannelStatusManuallyDisabled).Error)
	settings := `{"task_plugin_key":"doubao"}`
	channel := model.Channel{Name: "doubao-capabilities", Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled, Group: "default", Models: strings.Join(names, ","), Setting: &settings}
	require.NoError(t, db.Create(&channel).Error)
	for _, name := range names {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: channel.Id, Enabled: true}).Error)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-workspace/models", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var catalog struct {
		Data struct {
			Models []videoWorkspaceModel `json:"models"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &catalog))
	var actual []string
	for _, item := range catalog.Data.Models {
		actual = append(actual, item.ID)
		assert.False(t, item.SupportsImage, "Doubao uploads are unsupported by the current adapter")
	}
	assert.ElementsMatch(t, supported, actual, "the image-only model must not be offered without an image input")
}

func TestVideoWorkspaceProviderOnlyPricing(t *testing.T) {
	db, engine, calls := videoWorkspaceTestRouter(t)
	withSelfUseModeDisabled(t)
	oldPrices, oldRatios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	withTieredBillingConfig(t, map[string]string{"sora-2": "ratio"}, map[string]string{})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.plugin_billing_expr": `{"sora::sora-2":"tier(\"video\", u(\"seconds\") * 0.02)"}`}))
	require.False(t, helper.HasModelBillingConfig("sora-2"), "fixture has only a provider expression")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/video-workspace/models", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var catalog struct {
		Data struct {
			Models []videoWorkspaceModel `json:"models"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &catalog))
	require.Len(t, catalog.Data.Models, 1, "a configured provider expression is valid billing configuration")
	request := httptest.NewRequest(http.MethodPost, "http://workspace.example/api/video-workspace/tasks", strings.NewReader(`{"model":"sora-2","prompt":"provider-priced video","seconds":4}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://workspace.example")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var owner model.User
	require.NoError(t, db.First(&owner, 7).Error)
	assert.Equal(t, 1_000_000-common.QuotaRound(4*0.02*common.QuotaPerUnit), owner.Quota)
	assert.Equal(t, int32(1), calls.Load())
}
