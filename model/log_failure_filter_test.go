package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestLogFailureFilterDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres", "clickhouse"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			case "clickhouse":
				dsn := os.Getenv("TEST_CLICKHOUSE_DSN")
				if dsn == "" {
					t.Skip("TEST_CLICKHOUSE_DSN not configured")
				}
				driver = clickhouse.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			// Use a dedicated test database; fail rather than overwrite existing logs.
			require.False(t, db.Migrator().HasTable(&Log{}))
			previousLogDB, previousType := LOG_DB, common.LogDatabaseType()
			LOG_DB = db
			common.SetLogDatabaseType(common.DatabaseType(dialect))
			initCol()
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&Log{}))
				LOG_DB = previousLogDB
				common.SetLogDatabaseType(previousType)
				initCol()
			})
			if dialect == "clickhouse" {
				require.NoError(t, db.Exec(clickHouseLogCreateTableSQL(0)).Error)
			} else {
				require.NoError(t, db.AutoMigrate(&Log{}))
			}

			fixtures := []Log{
				{Type: LogTypeError, RequestId: "http-error", Other: `{"status_code":400}`},
				{Type: LogTypeConsume, RequestId: "client-gone", IsStream: true, Other: `{"stream_status":{"status":"error","end_reason":"client_gone"},"admin_info":{"private":true}}`},
				{Type: LogTypeConsume, RequestId: "timeout", IsStream: true, Other: `{ "stream_status": { "end_reason": "timeout", "status": "error" } }`},
				{Type: LogTypeConsume, RequestId: "ok", IsStream: true, Other: `{"stream_status":{"status":"ok"}}`},
				{Type: LogTypeConsume, RequestId: "empty"},
				{Type: LogTypeConsume, RequestId: "whitespace", Other: "   "},
				{Type: LogTypeConsume, RequestId: "null", Other: `null`},
				{Type: LogTypeConsume, RequestId: "unrelated-error", Other: `{"status":"error","admin_info":{"stream_status":{"status":"error"}}}`},
				{Type: LogTypeConsume, RequestId: "other-user", UserId: 200, IsStream: true, Other: `{"stream_status":{"status":"error"}}`},
				{Type: LogTypeSystem, RequestId: "system", Other: `{"stream_status":{"status":"error"}}`},
			}
			for i := range fixtures {
				fixtures[i].Id = i + 1
				fixtures[i].CreatedAt = 1700000000 + int64(i)
				fixtures[i].Username = "filter-user"
				fixtures[i].TokenName = "filter-token"
				fixtures[i].ModelName = "filter-model"
				fixtures[i].Group = "filter-group"
				if fixtures[i].UserId == 0 {
					fixtures[i].UserId = 100
				}
			}
			require.NoError(t, db.Create(&fixtures).Error)

			// List counts and pagination must include failures before applying LIMIT.
			logs, total, err := GetAllLogs(LogTypeError, 0, 0, "", "", "", 0, 2, 0, "", "", "")
			require.NoError(t, err)
			assert.EqualValues(t, 4, total)
			require.Len(t, logs, 2)
			assert.Equal(t, "other-user", logs[0].RequestId)
			assert.Equal(t, "timeout", logs[1].RequestId)
			logs, total, err = GetAllLogs(LogTypeError, 0, 0, "", "", "", 2, 2, 0, "", "", "")
			require.NoError(t, err)
			assert.EqualValues(t, 4, total)
			require.Len(t, logs, 2)
			assert.Equal(t, "client-gone", logs[0].RequestId)
			assert.Equal(t, "http-error", logs[1].RequestId)

			logs, total, err = GetUserLogs(100, LogTypeError, 0, 0, "", "", 0, 20, "", "", "")
			require.NoError(t, err)
			assert.EqualValues(t, 3, total)
			require.Len(t, logs, 3)
			for _, log := range logs {
				assert.Equal(t, 100, log.UserId, "OR must not bypass user isolation")
				assert.NotContains(t, log.Other, "admin_info")
			}
			assert.Contains(t, logs[1].Other, "client_gone")
			assert.Equal(t, LogTypeConsume, logs[1].Type)

			logs, total, err = GetAllLogs(LogTypeError, 1700000001, 1700000002, "filter-model", "filter-user", "filter-token", 0, 20, 0, "filter-group", "client-gone", "")
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, logs, 1)
			assert.Equal(t, "client-gone", logs[0].RequestId)

			for _, tc := range []struct {
				logType int
				total   int64
			}{{LogTypeConsume, 7}, {LogTypeUnknown, 9}, {LogTypeSystem, 1}} {
				logs, total, err = GetUserLogs(100, tc.logType, 0, 0, "", "", 0, 20, "", "", "")
				require.NoError(t, err)
				assert.Equal(t, tc.total, total)
				assert.Len(t, logs, int(tc.total))
			}
			var persisted Log
			require.NoError(t, db.Where("request_id = ?", "client-gone").First(&persisted).Error)
			assert.Equal(t, LogTypeConsume, persisted.Type)
			assert.Equal(t, fixtures[1].Other, persisted.Other)
		})
	}
}
