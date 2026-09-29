package plugins_test

import (
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay/channel"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMengwuxianPlugin(t *testing.T) (*jsplugin.Registry, *jsplugin.LoadedPlugin) {
	t.Helper()
	source, err := builtinplugins.Source("mengwuxian")
	require.NoError(t, err)
	registry := jsplugin.NewRegistry()
	plugin, err := registry.RegisterFactory(source, jsplugin.Options{Key: "mengwuxian"})
	require.NoError(t, err)
	return registry, plugin
}

func decodeMengwuxianRequest(t *testing.T, plugin *jsplugin.LoadedPlugin, protocol, upstream string, body map[string]any) (map[string]any, error) {
	t.Helper()
	value, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{protocol, "decodeRequest"}, map[string]any{
		"model": "client-alias", "upstreamModel": upstream, "operation": "generate", "body": body,
	})
	if err != nil {
		return nil, err
	}
	return decodePluginValue(t, value), nil
}

func mengwuxianTaskFixture(t *testing.T, plugin *jsplugin.LoadedPlugin, upstream, action string, request map[string]any) (*taskplugin.TaskAdaptor, *relaycommon.RelayInfo, *gin.Context) {
	t.Helper()
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://vendor.example", ApiKey: "test-api-key", UpstreamModelName: upstream},
		OriginModelName: "client-alias",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public", Action: action},
	}
	adaptor := taskplugin.New(plugin)
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", request)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	return adaptor, info, c
}

func TestMengwuxianBindingsAndSubmission(t *testing.T) {
	registry, plugin := newMengwuxianPlugin(t)
	for _, kind := range []string{"video", "image"} {
		for _, method := range []string{http.MethodPost, http.MethodGet} {
			path := "/mengwuxian/api/v1/" + kind + "-tasks"
			if method == http.MethodGet {
				path += "/:task_id"
			}
			binding, found := registry.Generation().LookupDeclaredRoute(method, path)
			require.True(t, found, path)
			assert.Same(t, plugin, binding.Plugin)
		}
		binding, found := registry.Generation().LookupDeclaredRoute(http.MethodPost, "/mengwuxian/api/v1/"+kind+"-tasks/multipart")
		require.True(t, found)
		assert.Same(t, plugin, binding.Plugin)
	}
	for _, tc := range []struct {
		model      string
		kind       string
		seconds    float64
		resolution string
		perTask    bool
	}{
		{"Sd-2.0满血933", "video", 4, "720p", false},
		{"minmax-h3", "video", 15, "2k", false},
		{"Sd-2.5", "video", 5, "720p", false},
		{"Sd-2.0fast", "video", 4, "720p", false},
		{"Sd-2.0mini", "video", 5, "480p", true},
		{"wan-3.0", "video", 5, "1080p", false},
		{"Nano_Banana_2img", "image", 0, "", true},
		{"gpt-image-2 z", "image", 0, "", true},
		{"gpt-image-2.5z", "image", 0, "", true},
		{"gpt-image-2.5-flare", "image", 0, "", true},
		{"gpt-image-2.5-sunburst", "image", 0, "", true},
	} {
		t.Run(tc.model, func(t *testing.T) {
			for _, endpoint := range []struct{ path, kind string }{{"/v1/videos", "video"}, {"/v1/images/generations", "image"}, {"/v1/images/edits", "image"}, {"/v1/responses", "responses"}} {
				binding, found := registry.Generation().LookupEndpoint(http.MethodPost, endpoint.path, tc.model)
				require.Equal(t, tc.kind == endpoint.kind, found, endpoint.path)
				if found {
					assert.Same(t, plugin, binding.Plugin)
				}
			}
			request := map[string]any{"model": "client-alias", "prompt": "a cat on a beach"}
			if tc.model == "minmax-h3" {
				request["input_reference"] = "https://cdn.example/reference.png"
			}
			intent, err := decodeMengwuxianRequest(t, plugin, "openai_"+tc.kind, tc.model, map[string]any{"kind": "json", "value": request})
			require.NoError(t, err)
			assert.Equal(t, "client-alias", intent["model"])
			wantAction := "image"
			if tc.kind == "video" {
				wantAction = "text_to_video"
				if tc.model == "minmax-h3" {
					wantAction = "image_to_video"
				}
			}
			assert.Equal(t, wantAction, intent["action"])
			adaptor, info, c := mengwuxianTaskFixture(t, plugin, tc.model, wantAction, intent["requestBody"].(map[string]any))
			reader, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			var sent map[string]any
			require.NoError(t, common.DecodeJson(reader, &sent))
			assert.Equal(t, tc.model, sent["model_code"])
			assert.Equal(t, "a cat on a beach", sent["prompt"])
			assert.NotContains(t, sent, "model", "the client alias must not replace the vendor model code")
			url, err := adaptor.BuildRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://vendor.example/api/v1/"+tc.kind+"-tasks", url)
			facts, err := adaptor.ExtractUsageFactsValidated(c, info)
			require.NoError(t, err)
			want := map[string]any{"image_count": float64(1)}
			if tc.kind == "video" {
				want = map[string]any{"resolution": tc.resolution, "with_audio": true}
				if tc.perTask {
					want["video_count"] = float64(1)
				} else {
					want["seconds"] = tc.seconds
				}
				assert.Equal(t, tc.seconds, sent["duration_seconds"])
				assert.Equal(t, tc.resolution, sent["resolution"])
			}
			assert.Equal(t, want, facts)
			schema, _ := plugin.Meta.UsageForModel(tc.model)
			assert.Len(t, schema, len(want))
			for key := range want {
				assert.Contains(t, schema, key)
			}
		})
	}

	for _, kind := range []string{"video", "image"} {
		upstream := "Sd-2.0fast"
		if kind == "image" {
			upstream = "gpt-image-2 z"
		}
		binding, found := registry.Generation().LookupDeclaredRoute(http.MethodPost, "/mengwuxian/api/v1/"+kind+"-tasks")
		require.True(t, found)
		value, err := plugin.Engine.CallPath(t.Context(), "native", []string{binding.Route.Decode}, map[string]any{
			"body": map[string]any{"kind": "json", "value": map[string]any{"model_code": upstream, "prompt": "a cat on a beach"}},
		})
		require.NoError(t, err)
		assert.Equal(t, upstream, decodePluginValue(t, value)["model"])
	}
}

func TestMengwuxianRequestValidation(t *testing.T) {
	_, plugin := newMengwuxianPlugin(t)
	for _, tc := range []struct {
		name   string
		model  string
		kind   string
		params map[string]any
	}{
		{"zero duration", "Sd-2.0fast", "video", map[string]any{"seconds": 0}},
		{"negative duration", "Sd-2.0fast", "video", map[string]any{"duration": -1}},
		{"fractional duration", "Sd-2.0fast", "video", map[string]any{"seconds": 4.5}},
		{"model duration limit", "Sd-2.0fast", "video", map[string]any{"seconds": 16}},
		{"maximum duration", "wan-3.0", "video", map[string]any{"duration_seconds": 31}},
		{"overflow duration", "Sd-2.5", "video", map[string]any{"duration_seconds": "18446744073686646784"}},
		{"duplicate duration aliases", "Sd-2.0fast", "video", map[string]any{"seconds": 4, "duration_seconds": 15}},
		{"unsupported resolution", "wan-3.0", "video", map[string]any{"resolution": "720p"}},
		{"conflicting size", "Sd-2.0fast", "video", map[string]any{"size": "1280x720", "resolution": "1080p"}},
		{"nonboolean audio", "Sd-2.0fast", "video", map[string]any{"with_audio": "false"}},
		{"required H3 reference", "minmax-h3", "video", nil},
		{"video references unsupported", "Sd-2.0fast", "video", map[string]any{"video_urls": []any{"https://cdn.example/ref.mp4"}}},
		{"unknown model", "unknown", "video", nil},
		{"video model on images", "Sd-2.0fast", "image", nil},
		{"image model on videos", "Nano_Banana_2img", "video", nil},
		{"zero image count", "Nano_Banana_2img", "image", map[string]any{"n": 0}},
		{"multiple images", "Nano_Banana_2img", "image", map[string]any{"n": 2}},
		{"oversized image count", "Nano_Banana_2img", "image", map[string]any{"image_count": 1e30}},
		{"duplicate image aliases", "Nano_Banana_2img", "image", map[string]any{"n": 1, "image_count": 1}},
		{"unconfirmed image tier", "gpt-image-2 z", "image", map[string]any{"resolution": "2k"}},
		{"unconfirmed image quality", "gpt-image-2 z", "image", map[string]any{"quality": "high"}},
		{"short image prompt", "gpt-image-2 z", "image", map[string]any{"prompt": "cat"}},
		{"unknown field", "Sd-2.0fast", "video", map[string]any{"future_option": true}},
		{"nested billing bypass", "Sd-2.0fast", "video", map[string]any{"metadata": map[string]any{"duration_seconds": 1000000}}},
		{"parameter billing bypass", "Sd-2.0fast", "video", map[string]any{"parameters": map[string]any{"duration_seconds": 1000000}}},
		{"generation count bypass", "gpt-image-2 z", "image", map[string]any{"generation_count": 10}},
		{"routing override", "Sd-2.0fast", "video", map[string]any{"route_key": "different-price"}},
		{"unsupported reference URL", "Sd-2.0fast", "video", map[string]any{"input_reference": "file:///tmp/image.png"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := map[string]any{"prompt": "a cat on a beach"}
			maps.Copy(request, tc.params)
			_, err := decodeMengwuxianRequest(t, plugin, "openai_"+tc.kind, tc.model, map[string]any{"kind": "json", "value": request})
			require.Error(t, err)
			action := "image"
			if tc.kind == "video" {
				action = "text_to_video"
			}
			_, err = plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{
				"model": "client-alias", "upstreamModel": tc.model, "action": action, "requestBody": request, "baseUrl": "https://vendor.example", "apiKey": "test-api-key",
			})
			assert.Error(t, err, "the driver must validate direct and passthrough input before billing")
		})
	}
	for _, kind := range []string{"form", "multipart"} {
		_, err := decodeMengwuxianRequest(t, plugin, "openai_video", "Sd-2.0fast", map[string]any{
			"kind": kind, "fields": map[string]any{"prompt": []string{"a cat on a beach"}, "seconds": []string{"4", "15"}},
		})
		assert.Error(t, err, "repeated scalar fields must not choose a cheaper billing value")
	}
	for _, seconds := range []int{4, 30} {
		intent, err := decodeMengwuxianRequest(t, plugin, "openai_video", "Sd-2.5", map[string]any{
			"kind": "json", "value": map[string]any{"prompt": "a cat on a beach", "seconds": seconds, "with_audio": false, "size": "1080x1920"},
		})
		require.NoError(t, err)
		adaptor, info, c := mengwuxianTaskFixture(t, plugin, "Sd-2.5", "text_to_video", intent["requestBody"].(map[string]any))
		facts, err := adaptor.ExtractUsageFactsValidated(c, info)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"seconds": float64(seconds), "resolution": "1080p", "with_audio": false}, facts)
	}
}

func TestMengwuxianMultipartReferences(t *testing.T) {
	_, plugin := newMengwuxianPlugin(t)
	for _, tc := range []struct{ kind, model, field string }{{"video", "minmax-h3", "input_reference"}, {"image", "Nano_Banana_2img", "image[]"}, {"image", "gpt-image-2 z", "reference_images"}} {
		t.Run(tc.field, func(t *testing.T) {
			files := []any{map[string]any{"ref": "request_file:" + tc.field, "field": tc.field, "filename": "image.png", "mimeType": "image/png", "size": 10 * 1024 * 1024}}
			body := map[string]any{"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat on a beach"}}, "files": files}
			intent, err := decodeMengwuxianRequest(t, plugin, "openai_"+tc.kind, tc.model, body)
			require.NoError(t, err)
			value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{
				"action": intent["action"], "model": "client-alias", "upstreamModel": tc.model, "baseUrl": "https://vendor.example", "apiKey": "test-api-key", "requestBody": intent["requestBody"], "files": files,
			})
			require.NoError(t, err)
			descriptor := decodePluginValue(t, value)
			assert.Equal(t, "https://vendor.example/api/v1/"+tc.kind+"-tasks/multipart", descriptor["url"])
			assert.Equal(t, "multipart", descriptor["bodyType"])
			parts := descriptor["parts"].([]any)
			assert.Contains(t, parts, map[string]any{"name": "reference_images", "fileRef": "request_file:" + tc.field, "filename": "image.png"})
			assert.Contains(t, parts, map[string]any{"name": "model_code", "value": tc.model})
		})
	}
	for _, tc := range []struct {
		name string
		file map[string]any
	}{
		{"oversized image", map[string]any{"field": "image", "mimeType": "image/png", "size": 10*1024*1024 + 1}},
		{"empty image", map[string]any{"field": "image", "mimeType": "image/png", "size": 0}},
		{"unsupported media type", map[string]any{"field": "image", "mimeType": "image/svg+xml", "size": 100}},
		{"unsupported audio upload", map[string]any{"field": "audio_files", "mimeType": "audio/mpeg", "size": 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeMengwuxianRequest(t, plugin, "openai_image", "Nano_Banana_2img", map[string]any{
				"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat on a beach"}}, "files": []any{tc.file},
			})
			assert.Error(t, err)
		})
	}
	for _, count := range []int{8, 9} {
		references := make([]any, count)
		for i := range references {
			references[i] = "https://cdn.example/reference.png"
		}
		_, err := decodeMengwuxianRequest(t, plugin, "openai_image", "gpt-image-2 z", map[string]any{
			"kind": "json", "value": map[string]any{"prompt": "a cat on a beach", "reference_image_urls": references},
		})
		if count == 8 {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	_, err := decodeMengwuxianRequest(t, plugin, "openai_image", "gpt-image-2 z", map[string]any{
		"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat on a beach"}, "reference_image_urls": []string{`["https://cdn.example/reference.png"]`}},
		"files": []any{map[string]any{"ref": "request_file:image", "field": "image", "filename": "image.png", "mimeType": "image/png", "size": 100}},
	})
	assert.Error(t, err, "mixed uploaded files and URL arrays are not supported by the verified vendor contract")
}

func TestMengwuxianTaskLifecycle(t *testing.T) {
	_, plugin := newMengwuxianPlugin(t)
	for _, tc := range []struct{ responseID, taskID string }{
		{`123`, "123"},
		{`"123"`, "123"},
		{`"tsk_0123456789abcdef0123456789abcdef"`, "tsk_0123456789abcdef0123456789abcdef"},
	} {
		adaptor, info, c := mengwuxianTaskFixture(t, plugin, "Sd-2.0fast", "text_to_video", map[string]any{"prompt": "a cat on a beach", "seconds": 8, "with_audio": false})
		parsed, taskErr := adaptor.ParseResponse(c, &http.Response{
			StatusCode: http.StatusAccepted, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"task_id":` + tc.responseID + `,"status":"pending"}`)),
		}, info)
		require.Nil(t, taskErr)
		require.NotNil(t, parsed)
		assert.Equal(t, tc.taskID, parsed.UpstreamTaskID)
		assert.JSONEq(t, `{"usage":{"seconds":8,"resolution":"720p","with_audio":false}}`, string(parsed.PluginState))
		task := &model.Task{TaskID: "task_public", Action: "text_to_video", Properties: model.Properties{OriginModelName: "client-alias", UpstreamModelName: "Sd-2.0fast"}, Data: parsed.TaskData,
			PrivateData: model.TaskPrivateData{UpstreamTaskID: parsed.UpstreamTaskID, PluginState: parsed.PluginState}}
		for _, state := range []struct{ status, want string }{{"pending", "QUEUED"}, {"submitted", "SUBMITTED"}, {"processing", "IN_PROGRESS"}, {"succeeded", "SUCCESS"}, {"failed", "FAILURE"}, {"canceled", "FAILURE"}, {"unexpected", "UNKNOWN"}} {
			body, err := common.Marshal(map[string]any{"task_id": tc.taskID, "status": state.status, "result_url": "https://cdn.example/video.mp4", "duration_seconds": 1e30, "charged_points": 1e30})
			require.NoError(t, err)
			result, err := adaptor.ParseTaskResult(task, &http.Response{StatusCode: http.StatusOK}, body)
			require.NoError(t, err)
			assert.Equal(t, state.want, result.Status)
			assert.Empty(t, result.UsageFacts, "polling must leave submitted duration and price dimensions frozen")
			assert.Empty(t, result.PluginState, "a polling snapshot must not replace submission facts")
		}
		value, err := plugin.Engine.Call(t.Context(), "buildQueryRequest", map[string]any{
			"action": task.Action, "model": task.Properties.OriginModelName, "upstreamModel": task.Properties.UpstreamModelName, "taskId": task.GetUpstreamTaskID(), "baseUrl": "https://vendor.example", "apiKey": "test-api-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "https://vendor.example/api/v1/video-tasks/"+tc.taskID, decodePluginValue(t, value)["url"])
	}
	for _, body := range []string{`{}`, `{"task_id":0}`, `{"task_id":-1}`, `{"task_id":1.5}`, `{"task_id":9007199254740992}`, `{"task_id":"../123"}`, `{"task_id":"tsk_0123456789abcdef0123456789abcdef/../other"}`, `{"task_id":"tsk_0123456789abcdef0123456789abcdef?key=x"}`} {
		adaptor, info, c := mengwuxianTaskFixture(t, plugin, "Sd-2.0fast", "text_to_video", map[string]any{"prompt": "a cat on a beach"})
		_, taskErr := adaptor.ParseResponse(c, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, info)
		assert.NotNil(t, taskErr, body)
	}
	_, err := plugin.Engine.Call(t.Context(), "buildQueryRequest", map[string]any{
		"action": "text_to_video", "model": "Sd-2.0fast", "taskId": "tsk_0123456789abcdef0123456789abcdef/../other", "baseUrl": "https://vendor.example", "apiKey": "test-api-key",
	})
	assert.Error(t, err, "polling must reject path injection from a persisted task ID")
	query, err := plugin.Engine.Call(t.Context(), "buildQueryRequest", map[string]any{
		"action": "text_to_video", "taskId": "tsk_0123456789abcdef0123456789abcdef", "baseUrl": "https://vendor.example", "apiKey": "test-api-key",
	})
	require.NoError(t, err, "retrieving an existing task must not require its original model metadata")
	assert.Equal(t, "https://vendor.example/api/v1/video-tasks/tsk_0123456789abcdef0123456789abcdef", decodePluginValue(t, query)["url"])
	for _, body := range []string{`{"status":"succeeded"}`, `{"status":"succeeded","result_url":""}`, `{"status":"succeeded","result_url":"javascript:alert(1)"}`, `{"status":"succeeded","task_id":124,"result_url":"https://cdn.example/image.png"}`} {
		adaptor := taskplugin.New(plugin)
		task := &model.Task{TaskID: "task_public", Action: "image", Properties: model.Properties{UpstreamModelName: "Nano_Banana_2img"}, PrivateData: model.TaskPrivateData{UpstreamTaskID: "123"}}
		result, err := adaptor.ParseTaskResult(task, &http.Response{StatusCode: http.StatusOK}, []byte(body))
		require.NoError(t, err)
		assert.Equal(t, "UNKNOWN", result.Status)
		assert.Empty(t, result.UsageFacts)
	}
	adaptor := taskplugin.New(plugin)
	result, err := adaptor.ParseTaskResult(&model.Task{Action: "image", Properties: model.Properties{UpstreamModelName: "gpt-image-2 z"}}, &http.Response{StatusCode: http.StatusOK}, []byte(`{"status":"succeeded","result_url":"https://cdn.example/image.png","image_count":9999}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"image_count": float64(1)}, result.UsageFacts)
}

func TestMengwuxianCredentialsAndArtifacts(t *testing.T) {
	_, plugin := newMengwuxianPlugin(t)
	ctx := map[string]any{"action": "text_to_video", "model": "Sd-2.0fast", "requestBody": map[string]any{"prompt": "a cat on a beach"}, "baseUrl": "https://vendor.example", "apiKey": "test-api-key", "publicTaskId": "task_public"}
	value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
	require.NoError(t, err)
	descriptor := decodePluginValue(t, value)
	assert.Equal(t, map[string]any{"X-API-Key": "test-api-key", "Content-Type": "application/json", "Idempotency-Key": "task_public"}, descriptor["headers"])
	for _, base := range []string{"http://vendor.example", "https://user:pass@vendor.example", "https://vendor.example?key=value", "https://vendor.example#fragment"} {
		invalid := maps.Clone(ctx)
		invalid["baseUrl"] = base
		_, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", invalid)
		assert.Error(t, err, base)
	}
	for _, key := range []string{"", "\r\nX-Other: injected"} {
		invalid := maps.Clone(ctx)
		invalid["apiKey"] = key
		_, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", invalid)
		assert.Error(t, err)
	}
	for _, kind := range []string{"image", "video"} {
		adaptor := taskplugin.New(plugin)
		adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://vendor.example", ApiKey: "test-api-key"}})
		action := "image"
		if kind == "video" {
			action = "text_to_video"
		}
		task := &model.Task{TaskID: "task_public", Action: action, Status: model.TaskStatusSuccess}
		task.SetData(map[string]any{"task_id": 123, "status": "succeeded", "result_url": "https://cdn.example/result"})
		artifacts, err := adaptor.ListArtifacts(task)
		require.NoError(t, err)
		assert.Equal(t, []channel.TaskArtifact{{Key: kind, Type: kind}}, artifacts)
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			content, err := adaptor.BuildContentRequest(task, kind, channel.TaskArtifactClientRequest{Method: method})
			require.NoError(t, err)
			require.NotNil(t, content)
			assert.Equal(t, "https://cdn.example/result", content.URL)
			assert.Equal(t, http.MethodGet, content.Method, "vendor result URLs are signed for GET, including client HEAD requests")
			assert.True(t, content.Credentialless)
			assert.Empty(t, content.Headers)
		}
		value, err := plugin.Engine.CallPath(t.Context(), "native", []string{"taskStatus"}, map[string]any{}, map[string]any{"task_id": "task_public", "status": "SUCCESS", "data": map[string]any{"task_id": 123, "result_url": "https://cdn.example/result"}})
		require.NoError(t, err)
		assert.Equal(t, "task_public", decodePluginValue(t, value)["task_id"])
	}

	fetchSettings := system_setting.GetFetchSetting()
	savedFetchSettings := *fetchSettings
	*fetchSettings = system_setting.FetchSetting{EnableSSRFProtection: true, AllowedPorts: []string{"80", "443", "444"}}
	t.Cleanup(func() { *fetchSettings = savedFetchSettings })
	service.InitHttpClient()
	client := service.GetHttpClient()
	require.NotNil(t, client.CheckRedirect)
	original, err := http.NewRequest(http.MethodPost, "https://8.8.8.8/api/v1/video-tasks", nil)
	require.NoError(t, err)
	original.Header.Set("X-API-Key", "test-api-key")
	for _, tc := range []struct {
		url     string
		allowed bool
	}{
		{"https://8.8.8.8/api/v1/video-tasks/", true},
		{"https://8.8.4.4/api/v1/video-tasks/", false},
		{"https://8.8.8.8:444/api/v1/video-tasks/", false},
		{"http://8.8.8.8/api/v1/video-tasks/", false},
	} {
		redirect, err := http.NewRequest(http.MethodGet, tc.url, nil)
		require.NoError(t, err)
		redirect.Header.Set("X-API-Key", "test-api-key")
		err = client.CheckRedirect(redirect, []*http.Request{original})
		if tc.allowed {
			assert.NoError(t, err)
		} else {
			assert.Error(t, err, "upstream credentials must not cross an origin or downgrade to HTTP")
		}
	}
	redirect, err := http.NewRequest(http.MethodGet, "https://8.8.4.4/api/v1/video-tasks/", nil)
	require.NoError(t, err)
	assert.Error(t, client.CheckRedirect(redirect, []*http.Request{original}), "the original API key must constrain redirects even if the redirected header is absent")
}
