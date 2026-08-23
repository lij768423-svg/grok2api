package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	modelapp "github.com/chenyme/grok2api/backend/internal/application/model"
	settingsapp "github.com/chenyme/grok2api/backend/internal/application/settings"
	"github.com/chenyme/grok2api/backend/internal/domain/account"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/repository"
	"github.com/chenyme/grok2api/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	service  *modelapp.Service
	settings *settingsapp.Service
}

const (
	modelSyncHeartbeatInterval = 15 * time.Second
	modelSyncWriteTimeout      = 30 * time.Second
)

// NewHandler accepts the settings service as an optional dependency so small
// transport tests and older embedders can continue to construct the model
// handler without runtime settings.
func NewHandler(service *modelapp.Service, settings ...*settingsapp.Service) *Handler {
	var settingsService *settingsapp.Service
	if len(settings) > 0 {
		settingsService = settings[0]
	}
	return &Handler{service: service, settings: settingsService}
}

func (h *Handler) Register(router *gin.RouterGroup) {
	router.GET("/models", h.list)
	router.GET("/models/groups", h.listGroups)
	router.GET("/models/accounts", h.listAccounts)
	router.POST("/models", h.create)
	router.POST("/models/sync", h.sync)
	router.PATCH("/models/batch", h.batchUpdate)
	router.DELETE("/models", h.batchDelete)
	router.PATCH("/models/:id", h.update)
	router.PATCH("/models/:id/quality-guard", h.updateQualityGuard)
	router.DELETE("/models/:id", h.delete)
}

type updateRequest struct {
	PublicID   *string   `json:"publicId"`
	Enabled    *bool     `json:"enabled"`
	AccountIDs *[]string `json:"accountIds"`
}

type createRequest struct {
	PublicID      string   `json:"publicId" binding:"required"`
	Provider      string   `json:"provider" binding:"required"`
	UpstreamModel string   `json:"upstreamModel" binding:"required"`
	Capability    string   `json:"capability" binding:"required"`
	Enabled       bool     `json:"enabled"`
	AccountIDs    []string `json:"accountIds"`
}

type batchUpdateRequest struct {
	IDs     []string `json:"ids" binding:"required"`
	Enabled bool     `json:"enabled"`
}

type batchDeleteRequest struct {
	IDs []string `json:"ids" binding:"required"`
}

type modelResponse struct {
	ID                uint64     `json:"id,string"`
	PublicID          string     `json:"publicId"`
	Provider          string     `json:"provider"`
	UpstreamModel     string     `json:"upstreamModel"`
	Capability        string     `json:"capability"`
	Enabled           bool       `json:"enabled"`
	Origin            string     `json:"origin"`
	AccountIDs        []string   `json:"accountIds"`
	BindingMode       bool       `json:"bindingMode"`
	SupportedAccounts int        `json:"supportedAccounts"`
	SyncedAccounts    int        `json:"syncedAccounts"`
	TotalAccounts     int        `json:"totalAccounts"`
	CapabilityKnown   bool       `json:"capabilityKnown"`
	Available         bool       `json:"available"`
	QualityGuardState string     `json:"qualityGuardState"`
	LastSyncedAt      *time.Time `json:"lastSyncedAt,omitempty"`
}

type modelGroupResponse struct {
	Key                  string          `json:"key"`
	Routes               []modelResponse `json:"routes"`
	EndpointCapabilities []string        `json:"endpointCapabilities"`
}

type accountOptionResponse struct {
	ID   uint64 `json:"id,string"`
	Name string `json:"name"`
}

func (h *Handler) list(c *gin.Context) {
	page, pageSize := pagination(c)
	activeScope, ok := parseOptionalBool(c.Query("activeScope"))
	if !ok {
		response.Error(c, http.StatusBadRequest, "invalidFilter", "activeScope 必须是 true 或 false")
		return
	}
	values, total, err := h.service.List(c.Request.Context(), page, pageSize, c.Query("search"), modelapp.ListFilter{Provider: c.Query("provider"), Providers: splitScopeQuery(c.QueryArray("providerScope")), Tiers: splitScopeQuery(c.QueryArray("tierScope")), Status: c.Query("status"), ActiveScope: activeScope, Sort: repository.SortQuery{Field: c.Query("sortBy"), Direction: repository.SortDirection(c.Query("sortOrder"))}})
	if errors.Is(err, modelapp.ErrInvalidFilter) {
		response.Error(c, http.StatusBadRequest, "invalidFilter", err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "modelListFailed", "读取模型失败")
		return
	}
	items := make([]modelResponse, 0, len(values))
	for _, value := range values {
		items = append(items, h.modelResponse(value))
	}
	response.Success(c, http.StatusOK, gin.H{"items": items, "page": page, "pageSize": pageSize, "total": total})
}

func (h *Handler) listGroups(c *gin.Context) {
	page, pageSize := pagination(c)
	values, total, err := h.service.ListGroups(c.Request.Context(), page, pageSize, c.Query("search"), modelapp.ListFilter{
		Provider: c.Query("provider"), Status: c.Query("status"),
		Sort: repository.SortQuery{Field: c.Query("sortBy"), Direction: repository.SortDirection(c.Query("sortOrder"))},
	})
	if errors.Is(err, modelapp.ErrInvalidFilter) {
		response.Error(c, http.StatusBadRequest, "invalidFilter", err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "modelListFailed", "读取模型能力组失败")
		return
	}
	items := make([]modelGroupResponse, 0, len(values))
	for _, value := range values {
		items = append(items, h.modelGroupResponse(value))
	}
	response.Success(c, http.StatusOK, gin.H{"items": items, "page": page, "pageSize": pageSize, "total": total})
}

func splitScopeQuery(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
	}
	return result
}

func parseOptionalBool(value string) (bool, bool) {
	switch strings.TrimSpace(value) {
	case "", "false":
		return false, true
	case "true":
		return true, true
	default:
		return false, false
	}
}

func (h *Handler) listAccounts(c *gin.Context) {
	values, err := h.service.ListBindableAccounts(c.Request.Context(), account.Provider(c.Query("provider")))
	if err != nil {
		h.writeServiceError(c, "modelAccountListFailed", err)
		return
	}
	items := make([]accountOptionResponse, 0, len(values))
	for _, value := range values {
		items = append(items, accountOptionResponse{ID: value.ID, Name: value.Name})
	}
	response.Success(c, http.StatusOK, gin.H{"items": items})
}

func (h *Handler) create(c *gin.Context) {
	var request createRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "请求参数无效")
		return
	}
	accountIDs, err := parseIDs(request.AccountIDs)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalidId", err.Error())
		return
	}
	value, err := h.service.Create(c.Request.Context(), modelapp.CreateInput{
		PublicID: request.PublicID, Provider: account.Provider(request.Provider), UpstreamModel: request.UpstreamModel,
		Capability: modeldomain.Capability(request.Capability), Enabled: request.Enabled, AccountIDs: accountIDs,
	})
	if err != nil {
		h.writeServiceError(c, "modelCreateFailed", err)
		return
	}
	response.Success(c, http.StatusCreated, h.modelResponse(value))
}

func (h *Handler) batchUpdate(c *gin.Context) {
	var request batchUpdateRequest
	if c.ShouldBindJSON(&request) != nil {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "请求参数无效")
		return
	}
	ids := make([]uint64, 0, len(request.IDs))
	for _, value := range request.IDs {
		id, err := strconv.ParseUint(value, 10, 64)
		if err != nil || id == 0 {
			response.Error(c, http.StatusBadRequest, "invalidId", fmt.Sprintf("无效模型 ID: %s", value))
			return
		}
		ids = append(ids, id)
	}
	updated, err := h.service.BatchSetEnabled(c.Request.Context(), ids, request.Enabled)
	if err != nil {
		h.writeServiceError(c, "modelBatchUpdateFailed", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"updated": updated})
}

func (h *Handler) batchDelete(c *gin.Context) {
	var request batchDeleteRequest
	if c.ShouldBindJSON(&request) != nil {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "请求参数无效")
		return
	}
	ids, err := parseIDs(request.IDs)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalidId", err.Error())
		return
	}
	deleted, err := h.service.BatchDelete(c.Request.Context(), ids)
	if err != nil {
		h.writeServiceError(c, "modelBatchDeleteFailed", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"deleted": deleted})
}

func (h *Handler) sync(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("X-Accel-Buffering", "no")
	if err := writeModelSyncComment(c, "connected"); err != nil {
		return
	}

	type syncResult struct {
		synced int
		err    error
	}
	result := make(chan syncResult, 1)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	var completed atomic.Int64
	var total atomic.Int64
	go func() {
		synced, err := h.service.SyncObserved(ctx, func(current, maximum int) {
			completed.Store(int64(current))
			total.Store(int64(maximum))
		})
		result <- syncResult{synced: synced, err: err}
	}()

	heartbeat := time.NewTicker(modelSyncHeartbeatInterval)
	defer heartbeat.Stop()
	progressTicker := time.NewTicker(250 * time.Millisecond)
	defer progressTicker.Stop()
	lastCompleted, lastTotal := -1, -1
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case value := <-result:
			if value.err != nil {
				_ = writeModelSyncEvent(c, "error", gin.H{"code": "modelSyncFailed", "message": "同步模型能力失败"})
				return
			}
			_ = writeModelSyncEvent(c, "complete", gin.H{"synced": value.synced})
			return
		case <-heartbeat.C:
			if err := writeModelSyncComment(c, "heartbeat"); err != nil {
				return
			}
		case <-progressTicker.C:
			current, maximum := int(completed.Load()), int(total.Load())
			if maximum > 0 && (current != lastCompleted || maximum != lastTotal) {
				if err := writeModelSyncEvent(c, "progress", gin.H{"completed": current, "total": maximum}); err != nil {
					return
				}
				lastCompleted, lastTotal = current, maximum
			}
		}
	}
}

func writeModelSyncEvent(c *gin.Context, event string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := setModelSyncWriteDeadline(c.Writer); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return err
	}
	c.Writer.Flush()
	return c.Request.Context().Err()
}

func writeModelSyncComment(c *gin.Context, comment string) error {
	if err := setModelSyncWriteDeadline(c.Writer); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, ": %s\n\n", comment); err != nil {
		return err
	}
	c.Writer.Flush()
	return c.Request.Context().Err()
}

func setModelSyncWriteDeadline(writer http.ResponseWriter) error {
	err := http.NewResponseController(writer).SetWriteDeadline(time.Now().Add(modelSyncWriteTimeout))
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func (h *Handler) update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, http.StatusBadRequest, "invalidId", "ID 无效")
		return
	}
	var request updateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "请求参数无效")
		return
	}
	var accountIDs *[]uint64
	if request.AccountIDs != nil {
		parsed, parseErr := parseIDs(*request.AccountIDs)
		if parseErr != nil {
			response.Error(c, http.StatusBadRequest, "invalidId", parseErr.Error())
			return
		}
		accountIDs = &parsed
	}
	value, err := h.service.Update(c.Request.Context(), id, modelapp.UpdateInput{PublicID: request.PublicID, Enabled: request.Enabled, AccountIDs: accountIDs})
	if err != nil {
		h.writeServiceError(c, "modelUpdateFailed", err)
		return
	}
	response.Success(c, http.StatusOK, h.modelResponse(value))
}

type qualityGuardRequest struct {
	State string `json:"state"`
}

func (h *Handler) updateQualityGuard(c *gin.Context) {
	if h.settings == nil {
		response.Error(c, http.StatusServiceUnavailable, "qualityGuardUnavailable", "质量守护模型策略暂不可用")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, http.StatusBadRequest, "invalidId", "ID 无效")
		return
	}
	route, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		h.writeServiceError(c, "modelQualityGuardFailed", err)
		return
	}
	if !isQualityGuardTextCapability(route.Capability) {
		response.Error(c, http.StatusBadRequest, "qualityGuardUnsupported", "媒体和语音模型不支持质量守护")
		return
	}
	var request qualityGuardRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, http.StatusBadRequest, "invalidRequest", "请求参数无效")
		return
	}
	state := modeldomain.NormalizeQualityGuardModelState(request.State)
	if strings.TrimSpace(request.State) != string(state) {
		response.Error(c, http.StatusBadRequest, "invalidQualityGuardState", "质量守护状态必须是 enabled、disabled 或 unknown")
		return
	}
	if _, err := h.settings.UpdateQualityRetryModelPolicy(c.Request.Context(), route.Provider, route.UpstreamModel, string(state)); err != nil {
		h.writeQualityGuardError(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{
		"state":         h.qualityGuardState(route),
		"provider":      string(route.Provider),
		"upstreamModel": modeldomain.DisplayUpstreamModel(route.Provider, route.UpstreamModel),
	})
}

func (h *Handler) delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, http.StatusBadRequest, "invalidId", "ID 无效")
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		h.writeServiceError(c, "modelDeleteFailed", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"deleted": true})
}

// writeServiceError 仅暴露明确的模型业务错误，避免泄露持久化细节。
func (h *Handler) writeServiceError(c *gin.Context, code string, err error) {
	switch {
	case errors.Is(err, modelapp.ErrInvalidInput):
		response.Error(c, http.StatusBadRequest, code, err.Error())
	case errors.Is(err, modelapp.ErrNotFound):
		response.Error(c, http.StatusNotFound, "modelNotFound", err.Error())
	case errors.Is(err, modelapp.ErrConflict):
		response.Error(c, http.StatusConflict, "modelConflict", err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, code, "模型操作失败")
	}
}

func (h *Handler) writeQualityGuardError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, settingsapp.ErrInvalidInput):
		response.Error(c, http.StatusBadRequest, "invalidQualityGuardState", err.Error())
	case errors.Is(err, settingsapp.ErrConflict):
		response.Error(c, http.StatusConflict, "settingsConflict", err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, "qualityGuardUpdateFailed", "保存质量守护模型策略失败")
	}
}

func (h *Handler) modelResponse(value modeldomain.Route) modelResponse {
	result := newModelResponse(value)
	result.QualityGuardState = h.qualityGuardState(value)
	return result
}

func (h *Handler) modelGroupResponse(value modelapp.RouteGroup) modelGroupResponse {
	routes := make([]modelResponse, 0, len(value.Routes))
	ids := make([]string, 0, len(value.Routes))
	for _, route := range value.Routes {
		routes = append(routes, h.modelResponse(route))
		ids = append(ids, strconv.FormatUint(route.ID, 10))
	}
	return modelGroupResponse{Key: strings.Join(ids, ":"), Routes: routes, EndpointCapabilities: append([]string(nil), value.EndpointCapabilities...)}
}

func (h *Handler) qualityGuardState(value modeldomain.Route) string {
	if !isQualityGuardTextCapability(value.Capability) {
		return string(modeldomain.QualityGuardModelDisabled)
	}
	if h.settings == nil {
		return string(modeldomain.DefaultQualityGuardModelState(value.Provider, value.UpstreamModel))
	}
	return string(h.settings.QualityRetryModelState(value.Provider, value.UpstreamModel))
}

func isQualityGuardTextCapability(value modeldomain.Capability) bool {
	switch value {
	case modeldomain.CapabilityResponses, modeldomain.CapabilityChat:
		return true
	default:
		return false
	}
}

func newModelResponse(value modeldomain.Route) modelResponse {
	manualBinding := len(value.BoundAccountIDs) > 0
	// Console uses a provider-wide static catalog, so catalog support is known
	// even when an account capability snapshot predates a newly shipped model.
	capabilityKnown := manualBinding || value.SyncedAccounts > 0 || (value.Provider == account.ProviderConsole && (value.Origin == modeldomain.OriginCatalog || value.SupportedAccounts > 0))
	available := value.TotalAccounts > 0 && (value.SupportedAccounts > 0 || (!manualBinding && value.SyncedAccounts < value.TotalAccounts))
	accountIDs := make([]string, 0, len(value.BoundAccountIDs))
	for _, id := range value.BoundAccountIDs {
		accountIDs = append(accountIDs, strconv.FormatUint(id, 10))
	}
	return modelResponse{
		ID: value.ID, PublicID: modeldomain.ExternalPublicID(value.Provider, value.PublicID), Provider: string(value.Provider), UpstreamModel: modeldomain.DisplayUpstreamModel(value.Provider, value.UpstreamModel), Capability: string(value.Capability),
		Enabled: value.Enabled, Origin: string(value.Origin), AccountIDs: accountIDs, BindingMode: manualBinding, SupportedAccounts: value.SupportedAccounts,
		SyncedAccounts: value.SyncedAccounts, TotalAccounts: value.TotalAccounts, CapabilityKnown: capabilityKnown,
		Available: available, QualityGuardState: string(modeldomain.DefaultQualityGuardModelState(value.Provider, value.UpstreamModel)), LastSyncedAt: value.LastSyncedAt,
	}
}

func newModelGroupResponse(value modelapp.RouteGroup) modelGroupResponse {
	routes := make([]modelResponse, 0, len(value.Routes))
	ids := make([]string, 0, len(value.Routes))
	for _, route := range value.Routes {
		routes = append(routes, newModelResponse(route))
		ids = append(ids, strconv.FormatUint(route.ID, 10))
	}
	return modelGroupResponse{Key: strings.Join(ids, ":"), Routes: routes, EndpointCapabilities: append([]string(nil), value.EndpointCapabilities...)}
}

func parseIDs(values []string) ([]uint64, error) {
	ids := make([]uint64, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseUint(value, 10, 64)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("无效 ID: %s", value)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func pagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	return repository.NormalizePage(page, pageSize, repository.DefaultPageSize)
}
