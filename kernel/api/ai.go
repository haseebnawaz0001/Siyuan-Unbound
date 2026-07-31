// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package api

import (
	"html"
	"net/http"

	"github.com/88250/gulu"
	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/conf"
	mcpclient "github.com/siyuan-note/siyuan/kernel/mcp/client"
	"github.com/siyuan-note/siyuan/kernel/model"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func chatGPT(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var msg string
	if !util.ParseJsonArgs(arg, ret, util.BindJsonArg("msg", &msg, true, true)) {
		return
	}
	ret.Data = model.ChatGPT(msg)
}

func chatGPTWithAction(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	idsArg := arg["ids"].([]any)
	var ids []string
	for _, id := range idsArg {
		ids = append(ids, id.(string))
	}
	action := arg["action"].(string)
	ret.Data = model.ChatGPTWithAction(ids, action)
}

// testModel tests the availability of an AI model. Using the provider's saved baseURL/APIKey/timeout, it verifies
// whether the specified model is available. It prefers an exact match against the model list fetched via
// ListModels; if that endpoint is unsupported, it falls back to a minimal Chat Completion call to verify
// connectivity.
func testModel(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var providerID, modelName string
	if !util.ParseJsonArgs(arg, ret,
		util.BindJsonArg("provider", &providerID, true, true),
		util.BindJsonArg("model", &modelName, true, true),
	) {
		return
	}

	// Look up the provider by ID (without restricting to enabled ones, so a not-yet-enabled config can be tested)
	var provider *conf.Provider
	for _, p := range model.Conf.AI.Providers {
		if p != nil && p.ID == providerID {
			provider = p
			break
		}
	}
	if nil == provider {
		ret.Code = -1
		ret.Msg = "provider not found"
		return
	}

	available, matched, err := util.TestModel(provider.APIKey, provider.BaseURL, modelName, provider.RequestTimeout)
	// Trim the available model list to the first 50 entries, to avoid an oversized response body
	if 50 < len(available) {
		available = available[:50]
	}
	// Test results are always returned with code=0; the specific success/failure info goes in data, and the
	// frontend controls how it's displayed, to avoid triggering the generic error toast that would leave the
	// button state unable to recover
	result := map[string]any{
		"available": available,
		"matched":   matched,
	}
	if nil != err {
		result["msg"] = err.Error()
		logging.LogErrorf("test model [%s] failed: %s", modelName, err)
	} else if !matched {
		result["msg"] = "model not in available list"
	}
	ret.Data = result
}

// testEmbeddingModel tests the availability of the embedding model. It reads the saved Embedding config directly,
// sends a minimal text embedding request to verify connectivity and authentication, and returns the vector
// dimensions for cross-checking.
func testEmbeddingModel(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	embedding := model.Conf.AI.Embedding
	if nil == embedding || "" == embedding.APIKey || "" == embedding.BaseURL || "" == embedding.Name {
		// When the config is incomplete, always return code=0 and put the info in data for the frontend to
		// display, to avoid a code=-1 triggering the generic error toast and leaving the frontend button unable
		// to recover
		ret.Data = map[string]any{
			"matched": false,
			"msg":     "embedding model not configured",
		}
		return
	}

	matched, dims, err := util.TestEmbeddingModel(embedding.APIKey, embedding.BaseURL, embedding.Name, embedding.Dimensions, embedding.Timeout)
	// Test results are always returned with code=0; the specific success/failure info goes in data, and the
	// frontend controls how it's displayed, to avoid triggering the generic error toast that would leave the
	// button state unable to recover
	result := map[string]any{
		"matched":    matched,
		"dimensions": dims,
	}
	if nil != err {
		result["msg"] = err.Error()
		logging.LogErrorf("test embedding model [%s] failed: %s", embedding.Name, err)
	}
	ret.Data = result
}

// testRerankModel tests the availability of the rerank model. It reads the saved Rerank config directly, and sends
// a single rerank request with a minimal query+documents payload to verify connectivity and authentication.
func testRerankModel(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	rerank := model.Conf.AI.Rerank
	if nil == rerank || "" == rerank.APIKey || "" == rerank.Endpoint || "" == rerank.Name {
		// When the config is incomplete, always return code=0 and put the info in data for the frontend to
		// display, to avoid a code=-1 triggering the generic error toast and leaving the frontend button unable
		// to recover
		ret.Data = map[string]any{
			"matched": false,
			"msg":     "rerank model not configured",
		}
		return
	}

	matched, err := util.TestRerankModel(rerank.APIKey, rerank.Endpoint, rerank.Name, rerank.Timeout)
	// Test results are always returned with code=0; the specific success/failure info goes in data for the frontend to display
	result := map[string]any{
		"matched": matched,
	}
	if nil != err {
		result["msg"] = err.Error()
		logging.LogErrorf("test rerank model [%s] failed: %s", rerank.Name, err)
	}
	ret.Data = result
}

// listModels fetches the given provider's list of available models (GET /v1/models), used to populate the model
// name dropdown in the frontend.
// A service that doesn't support this endpoint returns an error, and the frontend falls back to manual input.
func listModels(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}

	var providerID string
	if !util.ParseJsonArgs(arg, ret,
		util.BindJsonArg("provider", &providerID, true, true),
	) {
		return
	}

	var provider *conf.Provider
	for _, p := range model.Conf.AI.Providers {
		if p != nil && p.ID == providerID {
			provider = p
			break
		}
	}
	if nil == provider {
		ret.Code = -1
		ret.Msg = "provider not found"
		return
	}

	models, err := util.ListAvailableModels(provider.APIKey, provider.BaseURL, provider.RequestTimeout)
	result := map[string]any{
		"models": models,
	}
	if nil != err {
		result["msg"] = err.Error()
	}
	ret.Data = result
}

// embeddingStat returns embedding index progress statistics, used by the settings page to display a progress bar
// and various counts.
func embeddingStat(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)
	ret.Data = model.GetEmbeddingStat()
}

// mcpStatus returns the connection status of every configured MCP server, for the settings page to poll and display.
func mcpStatus(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)
	ret.Data = mcpclient.MCPStatus()
}

func mcpOAuthAuthorize(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}
	serverID, _ := arg["id"].(string)
	if model.Conf.AI == nil || model.Conf.AI.MCP == nil {
		ret.Code = -1
		ret.Msg = "MCP server not found"
		return
	}
	for _, server := range model.Conf.AI.MCP.Servers {
		if server.ID == serverID && server.Enabled && server.Type == "http" {
			mcpclient.ReconnectMCPAsync(model.Conf.AI.MCP.Servers, []string{serverID}, []string{serverID})
			return
		}
	}
	ret.Code = -1
	ret.Msg = "MCP server not found"
}

func mcpOAuthDisconnect(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)

	arg, ok := util.JsonArg(c, ret)
	if !ok {
		return
	}
	serverID, _ := arg["id"].(string)
	if err := mcpclient.DisconnectMCPOAuth(serverID); err != nil {
		ret.Code = -1
		ret.Msg = err.Error()
	}
	if model.Conf.AI != nil && model.Conf.AI.MCP != nil {
		mcpclient.ReconnectMCPAsync(model.Conf.AI.MCP.Servers, []string{serverID}, nil)
	}
}

func mcpOAuthCallback(c *gin.Context) {
	if !mcpclient.IsLoopbackCallback(c.Request.RemoteAddr) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	callbackError := c.Query("error")
	if err := mcpclient.CompleteMCPOAuth(c.Param("flowID"), c.Query("code"), c.Query("state"), callbackError); err != nil {
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", renderMCPOAuthCallbackPage(
			util.LangToBCP47(model.Conf.Lang), model.Conf.Language(327), model.Conf.Language(328), false))
		return
	}
	if callbackError != "" {
		c.Data(http.StatusOK, "text/html; charset=utf-8", renderMCPOAuthCallbackPage(
			util.LangToBCP47(model.Conf.Lang), model.Conf.Language(327), model.Conf.Language(328), false))
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", renderMCPOAuthCallbackPage(
		util.LangToBCP47(model.Conf.Lang), model.Conf.Language(325), model.Conf.Language(326), true))
}

func renderMCPOAuthCallbackPage(lang, title, message string, success bool) []byte {
	markClass, mark := " mark--error", "!"
	if success {
		markClass, mark = "", "✓"
	}
	return []byte(`<!doctype html><html lang="` + html.EscapeString(lang) + `"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1"><title>` + html.EscapeString(title) + `</title>` +
		`<style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f5f5f5;color:#202124;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}` +
		`main{box-sizing:border-box;width:min(480px,calc(100vw - 32px));padding:40px 32px;text-align:center;background:#fff;border:1px solid #ddd;border-radius:12px;box-shadow:0 8px 24px #00000014}` +
		`.mark{display:grid;place-items:center;width:56px;height:56px;margin:0 auto 24px;border-radius:50%;background:#2e7d32;color:#fff;font-size:32px}.mark--error{background:#c62828}` +
		`h1{margin:0 0 12px;font-size:24px;font-weight:600}p{margin:0;color:#5f6368;font-size:15px;line-height:1.7}` +
		`@media(prefers-color-scheme:dark){body{background:#171717;color:#eee}main{background:#242424;border-color:#3c3c3c;box-shadow:none}p{color:#bbb}}</style>` +
		`</head><body><main><div class="mark` + markClass + `" aria-hidden="true">` + mark + `</div><h1>` + html.EscapeString(title) +
		`</h1><p>` + html.EscapeString(message) + `</p></main></body></html>`)
}

// reindexEmbedding clears the embedding vector table and triggers the background indexer to recompute all blocks;
// runs asynchronously.
func reindexEmbedding(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)
	model.ReindexEmbedding()
}

// retryFailedEmbedding deletes the rows for failed blocks so they immediately return to the main loop for
// re-embedding, leaving already-succeeded vectors untouched; runs asynchronously.
func retryFailedEmbedding(c *gin.Context) {
	ret := gulu.Ret.NewResult()
	defer c.JSON(http.StatusOK, ret)
	model.RetryFailedEmbedding()
}
