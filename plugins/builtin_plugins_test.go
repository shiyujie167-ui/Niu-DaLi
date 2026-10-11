package plugins

import (
	"io/fs"
	"maps"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var expectedKeys = []string{"88api-seedance", "alibaba", "doubao", "google", "hailuo", "jimeng", "kling", "mengwuxian", "sora", "sunoapi", "vertex-ai", "vidu"}

func TestBuiltInVendorPluginsDeclareNativeRoutesAndLegacyChannelTypes(t *testing.T) {
	generation := jsplugin.DefaultRegistry.Generation()
	require.NotNil(t, generation)

	routes := []struct {
		method    string
		path      string
		key       string
		routeType jsplugin.RouteType
		action    string
		renderer  string
	}{
		{"POST", "/kling/v1/videos/text2video", "kling", jsplugin.RouteTypeSubmit, "text_to_video", "taskCreated"},
		{"POST", "/kling/v1/videos/image2video", "kling", jsplugin.RouteTypeSubmit, "image_to_video", "taskCreated"},
		{"GET", "/kling/v1/videos/text2video/:task_id", "kling", jsplugin.RouteTypeQuery, "", "taskStatus"},
		{"GET", "/kling/v1/videos/image2video/:task_id", "kling", jsplugin.RouteTypeQuery, "", "taskStatus"},
		{"POST", "/jimeng/", "jimeng", jsplugin.RouteTypeDynamic, "", "renderTask"},
		{"POST", "/suno/submit/:action", "sunoapi", jsplugin.RouteTypeSubmit, "", "renderSubmit"},
		{"POST", "/suno/fetch", "sunoapi", jsplugin.RouteTypeDynamic, "", "renderTasks"},
		{"GET", "/suno/fetch/:task_id", "sunoapi", jsplugin.RouteTypeQuery, "", "renderTask"},
		{"POST", "/doubao/api/v3/contents/generations/tasks", "doubao", jsplugin.RouteTypeSubmit, "", "taskCreated"},
		{"GET", "/doubao/api/v3/contents/generations/tasks/:task_id", "doubao", jsplugin.RouteTypeQuery, "", "taskStatus"},
		{"POST", "/doubao/api/v3/images/generations", "doubao", jsplugin.RouteTypeSubmit, "", "imageCreated"},
	}
	for _, expected := range routes {
		t.Run(expected.method+" "+expected.path, func(t *testing.T) {
			binding, found := generation.LookupDeclaredRoute(expected.method, expected.path)
			require.True(t, found)
			require.Equal(t, expected.key, binding.Plugin.Meta.Key)
			require.Equal(t, expected.routeType, binding.Route.Type)
			require.Equal(t, expected.action, binding.Route.Action)
			require.Equal(t, expected.renderer, binding.Route.Render)
		})
	}

	// The legacy Wan edit route serves both the wan2.5 image-list body and the
	// wanx2.1-imageedit function API.
	imageEdit, found := generation.LookupDeclaredRoute("POST", "/ali/api/v1/services/aigc/image2image/image-synthesis")
	require.True(t, found)
	assert.Equal(t, "alibaba", imageEdit.Plugin.Meta.Key)
	assert.ElementsMatch(t, []string{"wan2.5-i2i-preview", "wanx2.1-imageedit"}, imageEdit.Route.Models)

	channelTypes := []struct {
		value int
		key   string
	}{
		{1, "sora"},
		{36, "sunoapi"},
		{45, "doubao"},
		{50, "kling"},
		{51, "jimeng"},
		{54, "doubao"},
		{55, "sora"},
	}
	for _, channelType := range channelTypes {
		plugin, found := generation.GetByChannelType(channelType.value)
		require.True(t, found)
		require.Equal(t, channelType.key, plugin.Meta.Key)
	}
}

func loadSeedance88Plugin(t *testing.T) *jsplugin.LoadedPlugin {
	t.Helper()
	source, err := Source("88api-seedance")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "88api-seedance"})
	require.NoError(t, err)
	return plugin
}

func callSeedance88Hook(t *testing.T, plugin *jsplugin.LoadedPlugin, hook string, args ...any) map[string]any {
	t.Helper()
	value, err := plugin.Engine.Call(t.Context(), hook, args...)
	require.NoError(t, err)
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, common.Unmarshal(encoded, &result))
	return result
}

func decodeSeedance88Request(t *testing.T, plugin *jsplugin.LoadedPlugin, model string, body map[string]any) (map[string]any, error) {
	t.Helper()
	value, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{
		"model": model, "body": body,
	})
	if err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, common.Unmarshal(encoded, &result))
	return result, nil
}

func TestSeedance88VideoRequests(t *testing.T) {
	plugin := loadSeedance88Plugin(t)
	const model = "seedance-2.0官方版"

	t.Run("forwards every documented JSON parameter and explicit seed zero", func(t *testing.T) {
		request := map[string]any{
			"model": model, "prompt": "orbit the reference product", "duration": float64(8),
			"resolution": "720p", "ratio": "16:9", "images": []any{"https://images.example/product.jpg"},
			"negative_prompt": "blur", "seed": float64(0),
		}
		intent, err := decodeSeedance88Request(t, plugin, model, map[string]any{"kind": "json", "value": request})
		require.NoError(t, err)
		assert.Equal(t, "image_to_video", intent["action"])
		assert.Equal(t, request, intent["requestBody"])
		for _, upstream := range []string{"vendor", "new_api"} {
			descriptor := callSeedance88Hook(t, plugin, "buildSubmitRequest", map[string]any{
				"model": model, "upstreamModel": model, "requestBody": intent["requestBody"],
				"baseUrl": "https://api.88api.ai", "apiKey": "test-channel-key", "authHeader": "Bearer test-channel-key",
				"upstream": map[string]any{"kind": upstream},
			})
			assert.Equal(t, "https://api.88api.ai/v1/videos", descriptor["url"])
			assert.Equal(t, "POST", descriptor["method"])
			assert.Equal(t, request, descriptor["body"])
			assert.Equal(t, "Bearer test-channel-key", descriptor["headers"].(map[string]any)["Authorization"])
		}
	})

	t.Run("model mapping keeps the client identity and sends the upstream model", func(t *testing.T) {
		intentValue, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{
			"model": "customer-video", "upstreamModel": model,
			"body": map[string]any{"kind": "json", "value": map[string]any{"model": "customer-video", "prompt": "a cat"}},
		})
		require.NoError(t, err)
		intentData, err := common.Marshal(intentValue)
		require.NoError(t, err)
		var intent map[string]any
		require.NoError(t, common.Unmarshal(intentData, &intent))
		assert.Equal(t, "customer-video", intent["model"])
		descriptor := callSeedance88Hook(t, plugin, "buildSubmitRequest", map[string]any{
			"model": "customer-video", "upstreamModel": model, "requestBody": intent["requestBody"],
			"baseUrl": "https://api.88api.ai", "apiKey": "test-channel-key",
		})
		assert.NotContains(t, descriptor, "model")
		assert.Equal(t, model, descriptor["body"].(map[string]any)["model"])
	})

	t.Run("multipart forwards one opaque image reference and normalized parameters", func(t *testing.T) {
		file := map[string]any{"ref": "request_file:input_reference#0", "field": "input_reference", "filename": "product.webp", "mimeType": "image/webp", "size": 42}
		intent, err := decodeSeedance88Request(t, plugin, model, map[string]any{
			"kind": "multipart", "files": []any{file},
			"fields": map[string]any{
				"model": []string{model}, "prompt": []string{"orbit the product"}, "seconds": []string{"8"},
				"resolution": []string{"720p"}, "ratio": []string{"9:16"}, "negative_prompt": []string{"blur"}, "seed": []string{"0"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "image_to_video", intent["action"])
		descriptor := callSeedance88Hook(t, plugin, "buildSubmitRequest", map[string]any{
			"model": model, "upstreamModel": model, "requestBody": intent["requestBody"], "files": []any{file},
			"baseUrl": "https://api.88api.ai", "apiKey": "test-channel-key",
		})
		assert.Equal(t, "multipart", descriptor["bodyType"])
		parts, ok := descriptor["parts"].([]any)
		require.True(t, ok)
		values := map[string]any{}
		for _, partValue := range parts {
			part := partValue.(map[string]any)
			name := part["name"].(string)
			if name == "input_reference" {
				assert.Equal(t, file["ref"], part["fileRef"])
				assert.Equal(t, file["filename"], part["filename"])
			} else {
				values[name] = part["value"]
			}
		}
		assert.Equal(t, map[string]any{
			"model": model, "prompt": "orbit the product", "duration": float64(8), "resolution": "720p",
			"ratio": "9:16", "negative_prompt": "blur", "seed": float64(0),
		}, values)
		assert.Len(t, parts, len(values)+1)
	})

	t.Run("first and last frames survive JSON and multipart decoding", func(t *testing.T) {
		frames := map[string]any{"firstFrame": "https://images.example/start.jpg", "lastFrame": "https://images.example/end.jpg"}
		for _, body := range []map[string]any{
			{"kind": "json", "value": map[string]any{"model": model, "prompt": "smooth transition", "metadata": frames}},
			{"kind": "multipart", "fields": map[string]any{"model": []string{model}, "prompt": []string{"smooth transition"}, "metadata": []string{`{"firstFrame":"https://images.example/start.jpg","lastFrame":"https://images.example/end.jpg"}`}}},
		} {
			intent, err := decodeSeedance88Request(t, plugin, model, body)
			require.NoError(t, err)
			assert.Equal(t, "image_to_video", intent["action"])
			assert.Equal(t, frames, intent["requestBody"].(map[string]any)["metadata"])
		}
	})
}

func TestSeedance88VideoValidationAndUsage(t *testing.T) {
	plugin := loadSeedance88Plugin(t)
	const model = "seedance-2.0官方版"
	valid := []struct {
		model      string
		parameters map[string]any
		seconds    float64
		resolution string
	}{
		{"seedance-2.5官方版", map[string]any{}, 4, "720p"},
		{"seedance-2.5官方版", map[string]any{"duration": 30}, 30, "720p"},
		{model, map[string]any{"duration": 4}, 4, "720p"},
		{model, map[string]any{"duration": 15}, 15, "720p"},
		{"seedance-2.0-fast官方版", map[string]any{"seconds": "15"}, 15, "720p"},
		{"seedance-2.0-mini官方版", map[string]any{}, 5, "480p"},
		{"seedance-2.0-mini官方版", map[string]any{"duration": 15, "resolution": "720p"}, 15, "720p"},
	}
	for _, tc := range valid {
		t.Run(tc.model+" valid boundary", func(t *testing.T) {
			request := map[string]any{"model": tc.model, "prompt": "a cat"}
			maps.Copy(request, tc.parameters)
			intent, err := decodeSeedance88Request(t, plugin, tc.model, map[string]any{"kind": "json", "value": request})
			require.NoError(t, err)
			facts := callSeedance88Hook(t, plugin, "extractUsage", map[string]any{"model": tc.model, "upstreamModel": tc.model, "requestBody": intent["requestBody"]})
			assert.Equal(t, map[string]any{"seconds": tc.seconds, "resolution": tc.resolution}, facts)
			assert.Equal(t, tc.seconds, intent["requestBody"].(map[string]any)["duration"])
			assert.Equal(t, "16:9", intent["requestBody"].(map[string]any)["ratio"])
		})
	}

	invalid := []struct {
		name       string
		model      string
		parameters map[string]any
	}{
		{"below duration minimum", model, map[string]any{"duration": 3}},
		{"above duration maximum", model, map[string]any{"duration": 16}},
		{"above 2.5 duration maximum", "seedance-2.5官方版", map[string]any{"duration": 31}},
		{"fractional duration", model, map[string]any{"duration": 4.5}},
		{"string duration in JSON", model, map[string]any{"duration": "8"}},
		{"unbounded duration", model, map[string]any{"duration": 1e30}},
		{"numeric seconds", model, map[string]any{"seconds": 8}},
		{"fractional seconds", model, map[string]any{"seconds": "4.5"}},
		{"conflicting seconds and duration", model, map[string]any{"duration": 8, "seconds": "8"}},
		{"unsupported resolution", model, map[string]any{"resolution": "1080p"}},
		{"unsupported ratio", model, map[string]any{"ratio": "auto"}},
		{"callback is unsupported", model, map[string]any{"callback_url": "https://callback.example"}},
		{"audio is unsupported even false", model, map[string]any{"generate_audio": false}},
		{"unsupported parameter", model, map[string]any{"n": 2}},
		{"image alias", model, map[string]any{"image": "https://images.example/a.jpg"}},
		{"last frame without first", model, map[string]any{"metadata": map[string]any{"lastFrame": "https://images.example/end.jpg"}}},
		{"reference images and first frame", model, map[string]any{"images": []any{"https://images.example/a.jpg"}, "metadata": map[string]any{"firstFrame": "https://images.example/start.jpg"}}},
		{"mini frames unsupported", "seedance-2.0-mini官方版", map[string]any{"metadata": map[string]any{"firstFrame": "https://images.example/start.jpg"}}},
		{"metadata duration bypass", model, map[string]any{"metadata": map[string]any{"duration": 1e30}}},
		{"unsafe seed", model, map[string]any{"seed": 1e30}},
		{"fractional seed", model, map[string]any{"seed": 1.5}},
		{"missing prompt", model, map[string]any{"prompt": " "}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			request := map[string]any{"model": tc.model, "prompt": "a cat"}
			maps.Copy(request, tc.parameters)
			_, err := decodeSeedance88Request(t, plugin, tc.model, map[string]any{"kind": "json", "value": request})
			require.Error(t, err)
		})
	}

	t.Run("image limits follow the selected model", func(t *testing.T) {
		for _, tc := range []struct {
			model string
			limit int
		}{{model, 9}, {"seedance-2.5官方版", 30}} {
			images := make([]any, tc.limit+1)
			for i := range images {
				images[i] = "https://images.example/reference.jpg"
			}
			request := map[string]any{"model": tc.model, "prompt": "a cat", "images": images[:tc.limit]}
			_, err := decodeSeedance88Request(t, plugin, tc.model, map[string]any{"kind": "json", "value": request})
			require.NoError(t, err)
			request["images"] = images
			_, err = decodeSeedance88Request(t, plugin, tc.model, map[string]any{"kind": "json", "value": request})
			require.Error(t, err)
		}
	})

	t.Run("multipart rejects duplicated scalars and conflicting files", func(t *testing.T) {
		file := map[string]any{"ref": "request_file:input_reference", "field": "input_reference", "filename": "a.png", "mimeType": "image/png", "size": 42}
		for _, body := range []map[string]any{
			{"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat"}, "duration": []string{"8", "8"}}},
			{"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat"}, "duration": []string{"1e30"}}},
			{"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat"}, "duration": []string{"8"}, "seconds": []string{"8"}}},
			{"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat"}, "images": []string{`["https://images.example/a.jpg"]`}}, "files": []any{file}},
			{"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat"}}, "files": []any{file, file}},
			{"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat"}, "generate_audio": []string{"false"}}},
			{"kind": "multipart", "fields": map[string]any{"prompt": []string{"a cat"}, "metadata": []string{`{"firstFrame":"https://images.example/start.jpg"}`}}, "files": []any{file}},
		} {
			_, err := decodeSeedance88Request(t, plugin, model, body)
			require.Error(t, err)
		}
	})

	t.Run("driver and billing hooks defend against invalid normalized duration", func(t *testing.T) {
		ctx := map[string]any{"model": model, "upstreamModel": model, "baseUrl": "https://api.88api.ai", "apiKey": "test-channel-key", "requestBody": map[string]any{"model": model, "prompt": "a cat", "duration": 1e30}}
		for _, hook := range []string{"buildSubmitRequest", "extractUsage"} {
			_, err := plugin.Engine.Call(t.Context(), hook, ctx)
			require.Error(t, err, hook)
		}
	})

	t.Run("legacy per-call prices do not treat duration facts as multipliers", func(t *testing.T) {
		value, err := plugin.Engine.Call(t.Context(), "extractUsage", map[string]any{
			"model": model, "requestBody": map[string]any{"model": model, "prompt": "a cat", "duration": 8}, "usagePurpose": "billing_ratios",
		})
		require.NoError(t, err)
		assert.Nil(t, value)
	})
}

func TestSeedance88VideoLifecycle(t *testing.T) {
	plugin := loadSeedance88Plugin(t)
	const model = "seedance-2.0官方版"
	ctx := map[string]any{"model": model, "upstreamModel": model, "baseUrl": "https://api.88api.ai", "apiKey": "test-channel-key", "requestBody": map[string]any{"model": model, "prompt": "a cat", "duration": 8}}
	submission := callSeedance88Hook(t, plugin, "parseSubmitResponse", ctx, map[string]any{"statusCode": 200, "body": map[string]any{"id": "video_upstream", "status": "queued"}})
	assert.Equal(t, "video_upstream", submission["taskId"])
	_, err := plugin.Engine.Call(t.Context(), "parseSubmitResponse", ctx, map[string]any{"statusCode": 200, "body": map[string]any{"status": "queued"}})
	require.Error(t, err)

	t.Run("polling uses escaped id and upstream authentication", func(t *testing.T) {
		query := callSeedance88Hook(t, plugin, "buildQueryRequest", map[string]any{"baseUrl": ctx["baseUrl"], "apiKey": ctx["apiKey"], "taskId": "id/with?query"})
		assert.Equal(t, "https://api.88api.ai/v1/videos/id%2Fwith%3Fquery", query["url"])
		assert.Equal(t, "Bearer test-channel-key", query["headers"].(map[string]any)["Authorization"])
		for _, taskID := range []string{".", ".."} {
			_, err := plugin.Engine.Call(t.Context(), "buildQueryRequest", map[string]any{"baseUrl": ctx["baseUrl"], "apiKey": ctx["apiKey"], "taskId": taskID})
			require.Error(t, err, "dot segments must not change the upstream request path")
		}
	})

	t.Run("status determines success and preserves upstream failures", func(t *testing.T) {
		for _, tc := range []struct {
			status string
			want   string
		}{{"queued", "QUEUED"}, {"in_progress", "IN_PROGRESS"}, {"completed", "SUCCESS"}, {"failed", "FAILURE"}, {"unknown", "UNKNOWN"}, {"unexpected", "UNKNOWN"}, {"", "UNKNOWN"}} {
			result := callSeedance88Hook(t, plugin, "parseTaskResult", map[string]any{}, map[string]any{
				"status": tc.status, "progress": 100, "url": "https://cdn.example/video.mp4?signature=abc%2Bdef",
				"error": map[string]any{"code": "provider_error", "message": "video failed"},
			})
			assert.Equal(t, tc.want, result["status"], tc.status)
			if tc.want == "FAILURE" {
				assert.Contains(t, result["reason"], "video failed")
			}
		}
		rendered, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "render"}, map[string]any{}, map[string]any{"status": "FAILURE", "data": map[string]any{"status": "failed", "error": map[string]any{"code": "provider_error", "message": "video failed"}}})
		require.NoError(t, err)
		encoded, err := common.Marshal(rendered)
		require.NoError(t, err)
		var response map[string]any
		require.NoError(t, common.Unmarshal(encoded, &response))
		assert.Equal(t, map[string]any{"code": "provider_error", "message": "video failed"}, response["error"])
	})

	t.Run("completion never replaces the frozen submitted billing facts", func(t *testing.T) {
		for _, duration := range []any{0, -1, 4.5, 15, 1e30, "15"} {
			facts := callSeedance88Hook(t, plugin, "extractUsageOnComplete", map[string]any{"model": model, "state": submission["state"]}, map[string]any{"status": "SUCCESS"}, map[string]any{"status": "completed", "duration": duration, "resolution": "1080p"})
			assert.Empty(t, facts)
		}
	})

	t.Run("completed results retain a playable URL and only publish successful artifacts", func(t *testing.T) {
		for _, field := range []string{"url", "video_url", "result_url"} {
			result := callSeedance88Hook(t, plugin, "parseTaskResult", map[string]any{}, map[string]any{"status": "completed", field: "https://cdn.example/video.mp4"})
			assert.Equal(t, "SUCCESS", result["status"])
			assert.Equal(t, "https://cdn.example/video.mp4", result["url"])
		}
		for _, status := range []string{"QUEUED", "IN_PROGRESS", "FAILURE", "SUCCESS"} {
			value, err := plugin.Engine.Call(t.Context(), "listArtifacts", map[string]any{"status": status, "data": map[string]any{"url": "https://cdn.example/video.mp4"}})
			require.NoError(t, err)
			encoded, err := common.Marshal(value)
			require.NoError(t, err)
			var artifacts []map[string]any
			require.NoError(t, common.Unmarshal(encoded, &artifacts))
			if status == "SUCCESS" {
				require.Len(t, artifacts, 1)
				assert.Equal(t, "video", artifacts[0]["key"])
				assert.Equal(t, "video", artifacts[0]["type"])
			} else {
				assert.Empty(t, artifacts)
			}
		}
	})

	t.Run("content uses the result URL verbatim without any API credential", func(t *testing.T) {
		const videoURL = "https://cdn.example/video.mp4?signature=abc%2Bdef&expires=123"
		for _, field := range []string{"url", "video_url", "result_url"} {
			for _, method := range []string{"GET", "HEAD"} {
				content := callSeedance88Hook(t, plugin, "buildContentRequest", map[string]any{
					"baseUrl": ctx["baseUrl"], "apiKey": ctx["apiKey"], "authHeader": "Bearer test-channel-key", "artifactKey": "video",
					"data": map[string]any{field: videoURL}, "clientRequest": map[string]any{"method": method, "headers": map[string]any{"Authorization": "Bearer client-key"}},
				})
				assert.Equal(t, videoURL, content["url"])
				assert.Equal(t, "GET", content["method"], "the host suppresses the media body for client HEAD requests")
				assert.Equal(t, true, content["credentialless"])
				assert.Empty(t, content["headers"])
			}
		}
	})
}

func TestBuiltInTaskPluginResponsesAndUsageContracts(t *testing.T) {
	generation := jsplugin.DefaultRegistry.Generation()
	require.NotNil(t, generation)

	entries, err := fs.ReadDir(taskPlugins, "tasks")
	require.NoError(t, err)
	actualKeys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			actualKeys = append(actualKeys, entry.Name())
		}
	}
	assert.Equal(t, expectedKeys, actualKeys)

	for _, key := range expectedKeys {
		t.Run(key, func(t *testing.T) {
			_, found := generation.Get(key)
			require.True(t, found, "factory plugin was excluded from the active generation")

			source, sourceErr := Source(key)
			require.NoError(t, sourceErr)
			registry := jsplugin.NewRegistry()
			plugin, registerErr := registry.RegisterFactory(source, jsplugin.Options{Key: key})
			require.NoError(t, registerErr)

			var responsesClaim jsplugin.ProtocolClaim
			foundResponses := false
			for _, claim := range plugin.Meta.Protocols {
				if claim.Name == "openai_responses" {
					responsesClaim = claim
					foundResponses = true
					break
				}
			}
			require.Equal(t, key != "mengwuxian" && key != "88api-seedance", foundResponses, "plugins without a Responses surface must not claim its models")
			if foundResponses {
				assert.Equal(t, []string{"stream", "sync", "background"}, responsesClaim.Supports)
			}
			for _, model := range plugin.Meta.Models {
				binding, claimed := registry.Generation().LookupEndpoint("POST", "/v1/responses", model)
				require.Equal(t, foundResponses, claimed, model)
				if claimed {
					assert.Same(t, plugin, binding.Plugin)
				}
			}
			for _, hook := range []string{"decodeRequest", "renderEvents", "renderFinal"} {
				callable, callableErr := plugin.Engine.HasCallablePath(t.Context(), "protocols", "openai_responses", hook)
				require.NoError(t, callableErr)
				assert.Equal(t, foundResponses, callable, hook)
			}
			for _, hook := range []string{"extractUsage", "extractUsageOnComplete"} {
				callable, callableErr := plugin.Engine.HasExport(t.Context(), hook)
				require.NoError(t, callableErr)
				assert.True(t, callable, hook)
			}
			require.NotEmpty(t, plugin.Meta.UsageSchema)
			for usageKey, schema := range plugin.Meta.UsageSchema {
				assert.NotEmpty(t, schema.Description, usageKey)
			}
		})
	}
}

func TestBuiltInResponsesDecodersEchoChannelMappedAlias(t *testing.T) {
	bodyOverrides := map[string]map[string]any{}
	for _, key := range expectedKeys {
		t.Run(key, func(t *testing.T) {
			source, sourceErr := Source(key)
			require.NoError(t, sourceErr)
			registry := jsplugin.NewRegistry()
			plugin, registerErr := registry.RegisterFactory(source, jsplugin.Options{Key: key})
			require.NoError(t, registerErr)
			if key == "mengwuxian" || key == "88api-seedance" {
				return
			}
			require.NotEmpty(t, plugin.Meta.Models)

			alias := "alias-under-test"
			upstreamModel := plugin.Meta.Models[0]
			body := map[string]any{"model": alias, "input": "a cat walking on the beach"}
			if override, ok := bodyOverrides[key]; ok {
				body = override
			}
			value, callErr := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_responses", "decodeRequest"}, map[string]any{
				"model": alias, "upstreamModel": upstreamModel, "stream": false,
				"body": map[string]any{"kind": "json", "value": body},
			})
			require.NoError(t, callErr)
			encoded, marshalErr := common.Marshal(value)
			require.NoError(t, marshalErr)
			var decoded map[string]any
			require.NoError(t, common.Unmarshal(encoded, &decoded))
			assert.Equal(t, "submit", decoded["kind"])
			assert.Equal(t, alias, decoded["model"])
		})
	}
}

func TestBuiltInPluginsAddressNewAPIUpstreamOnNativeRoutes(t *testing.T) {
	generation := jsplugin.DefaultRegistry.Generation()
	require.NotNil(t, generation)
	for _, key := range []string{"hailuo", "google", "vidu", "vertex-ai"} {
		plugin, found := generation.Get(key)
		require.True(t, found, key)
		assert.False(t, plugin.Meta.SupportsUpstream(jsplugin.UpstreamKindNewAPI), "%s has no native routes and must not be bindable to a New API channel", key)
	}

	const base = "https://upstream.example"
	cases := []struct {
		key                string
		driver             map[string]any
		vendorKey          string
		vendorAuthPrefix   string
		vendorSubmit       string
		gatewaySubmit      string
		vendorQuery        string
		gatewayQuery       string
		content            string
		legacyKeyHeuristic bool
	}{
		{
			key:              "doubao",
			driver:           map[string]any{"action": "text_to_video", "model": "doubao-seedance-1-0-pro-250528", "upstreamModel": "doubao-seedance-1-0-pro-250528", "requestBody": map[string]any{"model": "doubao-seedance-1-0-pro-250528", "prompt": "a cat"}},
			vendorKey:        "vendor-key",
			vendorAuthPrefix: "Bearer vendor-key",
			vendorSubmit:     "/api/v3/contents/generations/tasks",
			gatewaySubmit:    "/doubao/api/v3/contents/generations/tasks",
			vendorQuery:      "/api/v3/contents/generations/tasks/tid",
			gatewayQuery:     "/doubao/api/v3/contents/generations/tasks/tid",
		},
		{
			key:              "alibaba",
			driver:           map[string]any{"action": "text_to_video", "model": "wan2.2-t2v-plus", "upstreamModel": "wan2.2-t2v-plus", "requestBody": map[string]any{"model": "wan2.2-t2v-plus", "prompt": "a cat"}},
			vendorKey:        "vendor-key",
			vendorAuthPrefix: "Bearer vendor-key",
			vendorSubmit:     "/api/v1/services/aigc/video-generation/video-synthesis",
			gatewaySubmit:    "/ali/api/v1/services/aigc/video-generation/video-synthesis",
			vendorQuery:      "/api/v1/tasks/tid",
			gatewayQuery:     "/ali/api/v1/tasks/tid",
		},
		{
			key:                "kling",
			driver:             map[string]any{"action": "text_to_video", "model": "kling-v1", "upstreamModel": "kling-v1", "requestBody": map[string]any{"prompt": "a cat"}},
			vendorKey:          "ak|sk",
			vendorAuthPrefix:   "Bearer ey",
			vendorSubmit:       "/v1/videos/text2video",
			gatewaySubmit:      "/kling/v1/videos/text2video",
			vendorQuery:        "/v1/videos/text2video/tid",
			gatewayQuery:       "/kling/v1/videos/text2video/tid",
			legacyKeyHeuristic: true,
		},
		{
			key:                "jimeng",
			driver:             map[string]any{"action": "text_to_video", "model": "jimeng_vgfm_t2v_l20", "upstreamModel": "jimeng_vgfm_t2v_l20", "requestBody": map[string]any{"prompt": "a cat"}},
			vendorKey:          "ak|sk",
			vendorAuthPrefix:   "HMAC-SHA256",
			vendorSubmit:       "/?Action=CVSync2AsyncSubmitTask&Version=2022-08-31",
			gatewaySubmit:      "/jimeng/?Action=CVSync2AsyncSubmitTask&Version=2022-08-31",
			vendorQuery:        "/?Action=CVSync2AsyncGetResult&Version=2022-08-31",
			gatewayQuery:       "/jimeng/?Action=CVSync2AsyncGetResult&Version=2022-08-31",
			legacyKeyHeuristic: true,
		},
		{
			key:              "sunoapi",
			driver:           map[string]any{"action": "MUSIC", "model": "suno_music", "upstreamModel": "suno_music", "requestBody": map[string]any{"prompt": "a song"}},
			vendorKey:        "vendor-key",
			vendorAuthPrefix: "Bearer vendor-key",
			vendorSubmit:     "/suno/submit/MUSIC",
			gatewaySubmit:    "/suno/submit/MUSIC",
			vendorQuery:      "/suno/fetch",
			gatewayQuery:     "/suno/fetch",
		},
		{
			key:              "sora",
			driver:           map[string]any{"action": "text_to_video", "model": "sora-2", "upstreamModel": "sora-2", "requestBody": map[string]any{"prompt": "a cat"}},
			vendorKey:        "vendor-key",
			vendorAuthPrefix: "Bearer vendor-key",
			vendorSubmit:     "/v1/videos",
			gatewaySubmit:    "/v1/videos",
			vendorQuery:      "/v1/videos/tid",
			gatewayQuery:     "/v1/videos/tid",
			content:          "/v1/videos/tid/content",
		},
	}
	variants := []struct {
		name      string
		upstream  map[string]any
		gateway   bool
		legacyKey bool
	}{
		{"vendor signal", map[string]any{"kind": "vendor"}, false, false},
		{"no signal from an older host", nil, false, false},
		{"new_api signal", map[string]any{"kind": "new_api"}, true, false},
		{"legacy sk- key without a signal", nil, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			plugin, found := generation.Get(tc.key)
			require.True(t, found)
			require.True(t, plugin.Meta.SupportsUpstream(jsplugin.UpstreamKindNewAPI), "%s must declare new_api to be bindable to a New API channel", tc.key)
			call := func(hook string, args ...any) map[string]any {
				value, err := plugin.Engine.Call(t.Context(), hook, args...)
				require.NoError(t, err, hook)
				encoded, err := common.Marshal(value)
				require.NoError(t, err)
				var descriptor map[string]any
				require.NoError(t, common.Unmarshal(encoded, &descriptor))
				return descriptor
			}
			for _, variant := range variants {
				if variant.legacyKey && !tc.legacyKeyHeuristic {
					continue
				}
				t.Run(variant.name, func(t *testing.T) {
					apiKey := tc.vendorKey
					if variant.legacyKey {
						apiKey = "sk-legacy"
					} else if variant.gateway {
						apiKey = "sk-gateway"
					}
					authHeader := apiKey
					if variant.gateway && !variant.legacyKey {
						authHeader = "Bearer " + apiKey
					}
					ctx := map[string]any{"baseUrl": base, "apiKey": apiKey, "authHeader": authHeader, "publicTaskId": "task_pub", "requestHeaders": map[string]any{}, "files": []any{}}
					for key, value := range tc.driver {
						ctx[key] = value
					}
					if variant.upstream != nil {
						ctx["upstream"] = variant.upstream
					}
					wantSubmit, wantQuery := tc.vendorSubmit, tc.vendorQuery
					if variant.gateway {
						wantSubmit, wantQuery = tc.gatewaySubmit, tc.gatewayQuery
					}

					submit := call("buildSubmitRequest", ctx)
					assert.Equal(t, base+wantSubmit, submit["url"])
					headers, _ := submit["headers"].(map[string]any)
					authorization, _ := headers["Authorization"].(string)
					if variant.gateway {
						assert.Equal(t, "Bearer "+apiKey, authorization, "a gateway receives its own token as a Bearer header")
					} else {
						assert.True(t, strings.HasPrefix(authorization, tc.vendorAuthPrefix), "vendor Authorization %q", authorization)
					}

					queryCtx := map[string]any{"taskId": "tid", "publicTaskId": "task_pub", "action": tc.driver["action"], "model": tc.driver["model"], "upstreamModel": tc.driver["upstreamModel"], "baseUrl": base, "apiKey": apiKey, "authHeader": authHeader, "data": nil, "state": nil}
					if variant.upstream != nil {
						queryCtx["upstream"] = variant.upstream
					}
					var query map[string]any
					if plugin.Meta.FetchMode == "batch" {
						query = call("buildBatchQueryRequest", queryCtx, []any{queryCtx})
					} else {
						query = call("buildQueryRequest", queryCtx)
					}
					assert.Equal(t, base+wantQuery, query["url"])

					if tc.content == "" {
						return
					}
					contentCtx := map[string]any{"baseUrl": base, "apiKey": apiKey, "authHeader": authHeader, "artifactKey": "video", "upstreamTaskId": "tid", "data": map[string]any{}, "clientRequest": map[string]any{"method": "GET", "headers": map[string]any{}}}
					if variant.upstream != nil {
						contentCtx["upstream"] = variant.upstream
					}
					assert.Equal(t, base+tc.content, call("buildContentRequest", contentCtx)["url"])
				})
			}
		})
	}

	t.Run("alibaba asks only DashScope for an interleaved image stream", func(t *testing.T) {
		plugin, found := generation.Get("alibaba")
		require.True(t, found)
		for _, variant := range []struct {
			name             string
			upstream         map[string]any
			wantResponseType string
			wantSSEHeader    bool
		}{
			{"vendor", map[string]any{"kind": "vendor"}, "sse", true},
			{"new_api", map[string]any{"kind": "new_api"}, "json", false},
		} {
			t.Run(variant.name, func(t *testing.T) {
				value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{
					"baseUrl": base, "apiKey": "key", "authHeader": "key", "publicTaskId": "task_pub", "upstream": variant.upstream,
					"model": "wan2.6-image", "upstreamModel": "wan2.6-image",
					"requestBody": map[string]any{"model": "wan2.6-image", "prompt": "a cat", "metadata": map[string]any{"upstream_mode": "sync", "parameters": map[string]any{"enable_interleave": true}}},
				})
				require.NoError(t, err)
				encoded, err := common.Marshal(value)
				require.NoError(t, err)
				var descriptor struct {
					URL          string            `json:"url"`
					ResponseType string            `json:"responseType"`
					Headers      map[string]string `json:"headers"`
				}
				require.NoError(t, common.Unmarshal(encoded, &descriptor))
				assert.Equal(t, variant.wantResponseType, descriptor.ResponseType, "a gateway native route aggregates the vendor stream and answers with JSON")
				_, hasSSEHeader := descriptor.Headers["X-DashScope-Sse"]
				assert.Equal(t, variant.wantSSEHeader, hasSSEHeader)
			})
		}
	})
}
