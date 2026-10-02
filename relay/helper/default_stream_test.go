package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDefaultStreamTestContext(enabled bool) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyTokenDefaultStream, enabled)
	return c
}

func TestApplyTokenDefaultStream(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		request dto.Request
		want    *bool
	}{
		{
			name:    "enables omitted OpenAI stream",
			enabled: true,
			request: &dto.GeneralOpenAIRequest{},
			want:    common.GetPointer(true),
		},
		{
			name:    "preserves explicit OpenAI false",
			enabled: true,
			request: &dto.GeneralOpenAIRequest{Stream: common.GetPointer(false)},
			want:    common.GetPointer(false),
		},
		{
			name:    "leaves disabled token unchanged",
			enabled: false,
			request: &dto.GeneralOpenAIRequest{},
			want:    nil,
		},
		{
			name:    "enables omitted Responses stream",
			enabled: true,
			request: &dto.OpenAIResponsesRequest{},
			want:    common.GetPointer(true),
		},
		{
			name:    "enables omitted Claude stream",
			enabled: true,
			request: &dto.ClaudeRequest{},
			want:    common.GetPointer(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyTokenDefaultStream(newDefaultStreamTestContext(tt.enabled), tt.request)

			switch request := tt.request.(type) {
			case *dto.GeneralOpenAIRequest:
				assert.Equal(t, tt.want, request.Stream)
			case *dto.OpenAIResponsesRequest:
				assert.Equal(t, tt.want, request.Stream)
			case *dto.ClaudeRequest:
				assert.Equal(t, tt.want, request.Stream)
			default:
				require.FailNow(t, "unexpected request type")
			}
		})
	}
}
