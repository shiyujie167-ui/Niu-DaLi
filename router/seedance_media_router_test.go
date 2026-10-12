package router

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSeedanceMediaRouteRequiresToken(t *testing.T) {
	router := gin.New()
	SetRelayRouter(router)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/media/uploads", strings.NewReader(`{"model":"seedance-2.5官方版"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
func TestSeedanceMediaRouteEnforcesModelPermissionBeforeUpstream(t *testing.T) {
	require.NoError(t, i18n.Init())
	router := gin.New()
	reached := false
	router.POST("/v1/media/uploads", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"unrelated-model": true})
	}, controller.PrepareSeedanceMediaUpload, middleware.Distribute(), func(c *gin.Context) { reached = true })
	req := httptest.NewRequest(http.MethodPost, "/v1/media/uploads", strings.NewReader(`{"model":"seedance-2.5官方版","size":1,"mime_type":"image/png","sha256":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.False(t, reached)
}
