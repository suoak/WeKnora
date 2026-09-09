package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type trackingSystemSettings struct {
	interfaces.SystemSettingService
	updates int
}

func (s *trackingSystemSettings) Update(context.Context, string, any) (*types.SystemSetting, error) {
	s.updates++
	return &types.SystemSetting{}, nil
}

type rejectingModelPolicy struct{ interfaces.ModelPolicyService }

func (*rejectingModelPolicy) ValidateModelForRole(context.Context, types.ModelPolicyRole, string) error {
	return errors.New("not a live builtin model")
}

func TestGenericSystemSettingAPICannotBypassModelPolicyValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	keys := []string{
		"model.default.chat_id", "model.default.summary_id", "model.default.embedding_id",
		"model.default.rerank_id", "model.default.vlm_id", "model.default.asr_id",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			settings := &trackingSystemSettings{}
			handler := &SystemHandler{systemSettingSvc: settings, modelPolicySvc: &rejectingModelPolicy{}}
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPut, "/api/v1/system/admin/settings/"+key,
				bytes.NewBufferString(`{"value":"private-model-id"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Params = gin.Params{{Key: "key", Value: key}}

			handler.UpdateSystemSetting(ctx)

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Zero(t, settings.updates)
		})
	}
}
