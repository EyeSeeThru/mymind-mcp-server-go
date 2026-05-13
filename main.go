// MyMind MCP Server — Go implementation
// Wraps MyMind REST API as a stdio MCP server.

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/your-username/mymind-mcp-server-go/jwt"
)

// ─── Version ────────────────────────────────────────────────────────────────

const version = "1.1.0"
const baseURL = "https://api.mymind.com"

// ─── Credential Loading ───────────────────────────────────────────────────────

func loadAccessKey() (accessKey string, baseURL string, err error) {
	baseURL = baseURL

	// 1. Env var
	if key := os.Getenv("MYMIND_ACCESS_KEY"); key != "" {
		return key, baseURL, nil
	}

	// 2. Default key file
	keyPath := filepath.Join(os.Getenv("HOME"), ".mymind_mcp_access_key")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data)), baseURL, nil
	}

	// 3. YAML config
	configPath := filepath.Join(os.Getenv("HOME"), ".mymind_mcp_config.yaml")
	if data, err := os.ReadFile(configPath); err == nil {
		// Simple YAML parse for access_key and optional base_url
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "access_key:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1]), baseURL, nil
				}
			}
			if strings.HasPrefix(line, "base_url:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					baseURL = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	return "", "", fmt.Errorf("no access key found: set MYMIND_ACCESS_KEY env var, create ~/.mymind_mcp_access_key, or use ~/.mymind_mcp_config.yaml")
}

func splitAccessKey(key string) (kid, secret string) {
	if idx := strings.Index(key, "."); idx != -1 {
		return key[:idx], key[idx+1:]
	}
	return "default", key
}

// ─── API Client ──────────────────────────────────────────────────────────────

type MyMindClient struct {
	accessKey string
	baseURL   string
	kid       string
	secret    string
}

func NewMyMindClient(accessKey, baseURL string) *MyMindClient {
	kid, secret := splitAccessKey(accessKey)
	return &MyMindClient{accessKey: accessKey, baseURL: baseURL, kid: kid, secret: secret}
}

func (c *MyMindClient) request(method, path string, body interface{}, params map[string]string) (map[string]interface{}, error) {
	reqURL := c.baseURL + path
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		reqURL += "?" + q.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(data))
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}

	token := jwt.Sign(c.kid, c.secret, path, method)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mymind-mcp-server-go/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var errBody map[string]interface{}
		json.Unmarshal(raw, &errBody)
		return nil, fmt.Errorf("MyMind API error %d: %v", resp.StatusCode, errBody)
	}

	var result map[string]interface{}
	if len(raw) > 0 {
		json.Unmarshal(raw, &result)
	}
	return result, nil
}

func (c *MyMindClient) requestRaw(method, path string, body interface{}, params map[string]string) ([]byte, string, error) {
	reqURL := c.baseURL + path
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		reqURL += "?" + q.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(data))
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, "", err
	}

	token := jwt.Sign(c.kid, c.secret, path, method)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "mymind-mcp-server-go/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if resp.StatusCode >= 400 {
		var errBody map[string]interface{}
		json.Unmarshal(raw, &errBody)
		return nil, "", fmt.Errorf("MyMind API error %d: %v", resp.StatusCode, errBody)
	}

	return raw, contentType, nil
}

// ─── Objects ────────────────────────────────────────────────────────────────

func (c *MyMindClient) ListObjects(q string, limit int) ([]interface{}, error) {
	params := map[string]string{"limit": fmt.Sprintf("%d", limit)}
	if q != "" {
		params["q"] = q
	}
	resp, err := c.request("GET", "/objects", nil, params)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []interface{}{}, nil
	}
	if arr, ok := resp["data"].([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{}, nil
}

func (c *MyMindClient) CreateObject(title, content, objURL string, tags, spaces []string) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if title != "" {
		body["title"] = title
	}
	if content != "" {
		body["content"] = content
	}
	if objURL != "" {
		body["url"] = objURL
	}
	if len(tags) > 0 {
		tagObjs := make([]map[string]string, len(tags))
		for i, t := range tags {
			tagObjs[i] = map[string]string{"name": t}
		}
		body["tags"] = tagObjs
	}
	if len(spaces) > 0 {
		spaceObjs := make([]map[string]string, len(spaces))
		for i, s := range spaces {
			spaceObjs[i] = map[string]string{"id": s}
		}
		body["spaces"] = spaceObjs
	}
	return c.request("POST", "/objects", body, nil)
}

func (c *MyMindClient) GetObject(id, contentAs string) (map[string]interface{}, error) {
	params := map[string]string{}
	if contentAs != "" {
		params["contentAs"] = contentAs
	}
	return c.request("GET", "/objects/"+id, nil, params)
}

func (c *MyMindClient) UpdateObject(id, title string) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if title != "" {
		body["title"] = title
	}
	return c.request("PATCH", "/objects/"+id, body, nil)
}

func (c *MyMindClient) DeleteObject(id string) (map[string]interface{}, error) {
	return c.request("DELETE", "/objects/"+id, nil, nil)
}

func (c *MyMindClient) DownloadObject(id string) (map[string]interface{}, error) {
	path := "/objects/" + id + "/download"
	token := jwt.Sign(c.kid, c.secret, path, "GET")
	reqURL := c.baseURL + path
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "mymind-mcp-server-go/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var errBody map[string]interface{}
		json.Unmarshal(raw, &errBody)
		return nil, fmt.Errorf("MyMind API error %d: %v", resp.StatusCode, errBody)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	dataB64 := base64.StdEncoding.EncodeToString(raw)
	return map[string]interface{}{
		"content_type": contentType,
		"data":         dataB64,
	}, nil
}

func (c *MyMindClient) PinObject(id string) (map[string]interface{}, error) {
	return c.request("POST", "/objects/"+id+"/pin", nil, nil)
}

func (c *MyMindClient) RestoreObject(id string) (map[string]interface{}, error) {
	return c.request("POST", "/objects/"+id+"/restore", nil, nil)
}

// ─── Search ─────────────────────────────────────────────────────────────────

func (c *MyMindClient) Search(query string, limit int) ([]interface{}, error) {
	params := map[string]string{"q": query, "limit": fmt.Sprintf("%d", limit)}
	resp, err := c.request("GET", "/search", nil, params)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []interface{}{}, nil
	}
	if arr, ok := resp["data"].([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{}, nil
}

func (c *MyMindClient) SemanticSearch(query string, limit int) ([]interface{}, error) {
	params := map[string]string{"q": query, "limit": fmt.Sprintf("%d", limit)}
	resp, err := c.request("GET", "/search/semantic", nil, params)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []interface{}{}, nil
	}
	if arr, ok := resp["data"].([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{}, nil
}

// ─── Blob / Thumbnail / Screenshot ──────────────────────────────────────────

func (c *MyMindClient) GetBlob(id string) (map[string]interface{}, error) {
	raw, contentType, err := c.requestRaw("GET", "/objects/"+id+"/blob", nil, nil)
	if err != nil {
		return nil, err
	}
	dataB64 := base64.StdEncoding.EncodeToString(raw)
	return map[string]interface{}{
		"content_type": contentType,
		"data":         dataB64,
	}, nil
}

func (c *MyMindClient) GetThumbnail(id string) (map[string]interface{}, error) {
	raw, contentType, err := c.requestRaw("GET", "/objects/"+id+"/thumbnail", nil, nil)
	if err != nil {
		return nil, err
	}
	dataB64 := base64.StdEncoding.EncodeToString(raw)
	return map[string]interface{}{
		"content_type": contentType,
		"data":         dataB64,
	}, nil
}

func (c *MyMindClient) GetScreenshot(id string) (map[string]interface{}, error) {
	raw, contentType, err := c.requestRaw("GET", "/objects/"+id+"/screenshot", nil, nil)
	if err != nil {
		return nil, err
	}
	dataB64 := base64.StdEncoding.EncodeToString(raw)
	return map[string]interface{}{
		"content_type": contentType,
		"data":         dataB64,
	}, nil
}

// ─── Convert ────────────────────────────────────────────────────────────────

func (c *MyMindClient) ConvertObject(id, toFormat string) (map[string]interface{}, error) {
	body := map[string]interface{}{"format": toFormat}
	return c.request("POST", "/objects/"+id+"/convert", body, nil)
}

// ─── Notes ───────────────────────────────────────────────────────────────────

func (c *MyMindClient) CreateNote(title, content string) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if title != "" {
		body["title"] = title
	}
	if content != "" {
		body["content"] = content
	}
	return c.request("POST", "/notes", body, nil)
}

func (c *MyMindClient) GetNote(id string) (map[string]interface{}, error) {
	return c.request("GET", "/notes/"+id, nil, nil)
}

func (c *MyMindClient) UpdateNote(id, title, content string) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if title != "" {
		body["title"] = title
	}
	if content != "" {
		body["content"] = content
	}
	return c.request("PATCH", "/notes/"+id, body, nil)
}

func (c *MyMindClient) DeleteNote(id string) (map[string]interface{}, error) {
	return c.request("DELETE", "/notes/"+id, nil, nil)
}

func (c *MyMindClient) ListNotes(limit int) ([]interface{}, error) {
	params := map[string]string{"limit": fmt.Sprintf("%d", limit)}
	resp, err := c.request("GET", "/notes", nil, params)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []interface{}{}, nil
	}
	if arr, ok := resp["data"].([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{}, nil
}

// ─── Links ──────────────────────────────────────────────────────────────────

func (c *MyMindClient) AddLink(sourceID, targetID, linkType string) (map[string]interface{}, error) {
	body := map[string]interface{}{"target_id": targetID}
	if linkType != "" {
		body["type"] = linkType
	}
	return c.request("POST", "/objects/"+sourceID+"/links", body, nil)
}

func (c *MyMindClient) RemoveLink(sourceID, linkID string) (map[string]interface{}, error) {
	return c.request("DELETE", "/objects/"+sourceID+"/links/"+linkID, nil, nil)
}

func (c *MyMindClient) ListLinks(id string) ([]interface{}, error) {
	resp, err := c.request("GET", "/objects/"+id+"/links", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []interface{}{}, nil
	}
	if arr, ok := resp["data"].([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{}, nil
}

// ─── Space Membership ───────────────────────────────────────────────────────

func (c *MyMindClient) AddToSpace(objectID, spaceID string) (map[string]interface{}, error) {
	body := map[string]interface{}{"id": spaceID}
	return c.request("POST", "/objects/"+objectID+"/spaces", body, nil)
}

func (c *MyMindClient) RemoveFromSpace(objectID, spaceID string) (map[string]interface{}, error) {
	return c.request("DELETE", "/objects/"+objectID+"/spaces/"+spaceID, nil, nil)
}

// ─── Tags ────────────────────────────────────────────────────────────────────

func (c *MyMindClient) AddTags(id string, tags []string) (map[string]interface{}, error) {
	body := make([]map[string]string, len(tags))
	for i, t := range tags {
		body[i] = map[string]string{"name": t}
	}
	return c.request("POST", "/objects/"+id+"/tags", body, nil)
}

func (c *MyMindClient) RemoveTag(id, tag string) (map[string]interface{}, error) {
	return c.request("DELETE", "/objects/"+id+"/tags/"+tag, nil, nil)
}

func (c *MyMindClient) ListTags() ([]interface{}, error) {
	resp, err := c.request("GET", "/tags", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []interface{}{}, nil
	}
	if arr, ok := resp["data"].([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{}, nil
}

// ─── Spaces ─────────────────────────────────────────────────────────────────

func (c *MyMindClient) ListSpaces() ([]interface{}, error) {
	resp, err := c.request("GET", "/spaces", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []interface{}{}, nil
	}
	if arr, ok := resp["data"].([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{}, nil
}

func (c *MyMindClient) CreateSpace(name string) (map[string]interface{}, error) {
	return c.request("POST", "/spaces", map[string]string{"name": name}, nil)
}

func (c *MyMindClient) GetSpace(id string) (map[string]interface{}, error) {
	return c.request("GET", "/spaces/"+id, nil, nil)
}

func (c *MyMindClient) DeleteSpace(id string) (map[string]interface{}, error) {
	return c.request("DELETE", "/spaces/"+id, nil, nil)
}

// ─── Related (fixed endpoint) ───────────────────────────────────────────────

func (c *MyMindClient) Related(id string, limit int) ([]interface{}, error) {
	params := map[string]string{"limit": fmt.Sprintf("%d", limit)}
	// Fixed: use /objects/{id}/related instead of /objects/id/related
	resp, err := c.request("GET", "/objects/"+id+"/related", nil, params)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []interface{}{}, nil
	}
	if arr, ok := resp["data"].([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{}, nil
}

// ─── Tool Handlers ───────────────────────────────────────────────────────────

type toolHandler func(client *MyMindClient, args map[string]interface{}) (interface{}, error)

var tools = map[string]toolHandler{
	// Objects
	"list_objects": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		q := getString(args, "q")
		limit := getInt(args, "limit", 100)
		return c.ListObjects(q, limit)
	},
	"create_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		tags := toStringArray(args["tags"])
		spaces := toStringArray(args["spaces"])
		return c.CreateObject(getString(args, "title"), getString(args, "content"), getString(args, "url"), tags, spaces)
	},
	"get_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetObject(getString(args, "id"), getString(args, "contentAs"))
	},
	"update_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.UpdateObject(getString(args, "id"), getString(args, "title"))
	},
	"delete_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.DeleteObject(getString(args, "id"))
	},
	"download_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.DownloadObject(getString(args, "id"))
	},
	"pin_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.PinObject(getString(args, "id"))
	},
	"restore_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.RestoreObject(getString(args, "id"))
	},

	// Search
	"search": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.Search(getString(args, "query"), getInt(args, "limit", 20))
	},
	"semantic_search": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.SemanticSearch(getString(args, "query"), getInt(args, "limit", 20))
	},

	// Blob / Thumbnail / Screenshot
	"get_blob": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetBlob(getString(args, "id"))
	},
	"get_thumbnail": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetThumbnail(getString(args, "id"))
	},
	"get_screenshot": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetScreenshot(getString(args, "id"))
	},

	// Convert
	"convert_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.ConvertObject(getString(args, "id"), getString(args, "format"))
	},

	// Notes CRUD
	"create_note": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.CreateNote(getString(args, "title"), getString(args, "content"))
	},
	"get_note": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetNote(getString(args, "id"))
	},
	"update_note": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.UpdateNote(getString(args, "id"), getString(args, "title"), getString(args, "content"))
	},
	"delete_note": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.DeleteNote(getString(args, "id"))
	},
	"list_notes": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.ListNotes(getInt(args, "limit", 100))
	},

	// Links
	"add_link": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.AddLink(getString(args, "id"), getString(args, "target_id"), getString(args, "type"))
	},
	"remove_link": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.RemoveLink(getString(args, "id"), getString(args, "link_id"))
	},
	"list_links": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.ListLinks(getString(args, "id"))
	},

	// Space membership
	"add_to_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.AddToSpace(getString(args, "id"), getString(args, "space_id"))
	},
	"remove_from_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.RemoveFromSpace(getString(args, "id"), getString(args, "space_id"))
	},

	// Tags
	"add_tags": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.AddTags(getString(args, "id"), toStringArray(args["tags"]))
	},
	"remove_tag": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.RemoveTag(getString(args, "id"), getString(args, "tag"))
	},
	"list_tags": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.ListTags()
	},

	// Spaces
	"list_spaces": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.ListSpaces()
	},
	"create_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.CreateSpace(getString(args, "name"))
	},
	"get_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetSpace(getString(args, "id"))
	},
	"delete_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.DeleteSpace(getString(args, "id"))
	},

	// Related
	"related": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.Related(getString(args, "id"), getInt(args, "limit", 20))
	},
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func getString(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func getInt(args map[string]interface{}, key string, defaultVal int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

func toStringArray(v interface{}) []string {
	if v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// ─── MCP Protocol ─────────────────────────────────────────────────────────────

var toolManifest = []map[string]interface{}{
	// Objects
	{"name": "list_objects", "description": "List objects from MyMind. Optional: q (search query), limit", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer", "default": 100}}}},
	{"name": "create_object", "description": "Create a new object (URL, note, or content) in MyMind.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"title": map[string]interface{}{"type": "string"}, "content": map[string]interface{}{"type": "string"}, "url": map[string]interface{}{"type": "string"}, "tags": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}, "spaces": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}}, "required": []interface{}{}}},
	{"name": "get_object", "description": "Get a single object by ID. Optional: contentAs", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "contentAs": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "update_object", "description": "Update an object's title.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "title": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "delete_object", "description": "Delete an object by ID.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "download_object", "description": "Download object content (returns base64).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "pin_object", "description": "Pin an object by ID.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "restore_object", "description": "Restore a pinned object by ID.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},

	// Search
	{"name": "search", "description": "Search MyMind objects by keyword.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer", "default": 20}}, "required": []interface{}{"query"}}},
	{"name": "semantic_search", "description": "Search MyMind objects by semantic similarity.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer", "default": 20}}, "required": []interface{}{"query"}}},

	// Blob / Thumbnail / Screenshot
	{"name": "get_blob", "description": "Get the raw blob data for an object (returns base64).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "get_thumbnail", "description": "Get the thumbnail image for an object (returns base64).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "get_screenshot", "description": "Get the screenshot image for an object (returns base64).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},

	// Convert
	{"name": "convert_object", "description": "Convert an object to a different format.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "format": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id", "format"}}},

	// Notes CRUD
	{"name": "create_note", "description": "Create a new note.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"title": map[string]interface{}{"type": "string"}, "content": map[string]interface{}{"type": "string"}}, "required": []interface{}{}}},
	{"name": "get_note", "description": "Get a single note by ID.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "update_note", "description": "Update a note's title and/or content.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "title": map[string]interface{}{"type": "string"}, "content": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "delete_note", "description": "Delete a note by ID.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "list_notes", "description": "List all notes. Optional: limit", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"limit": map[string]interface{}{"type": "integer", "default": 100}}}},

	// Links
	{"name": "add_link", "description": "Add a link from one object to another.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "target_id": map[string]interface{}{"type": "string"}, "type": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id", "target_id"}}},
	{"name": "remove_link", "description": "Remove a link from an object.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "link_id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id", "link_id"}}},
	{"name": "list_links", "description": "List all links for an object.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},

	// Space membership
	{"name": "add_to_space", "description": "Add an object to a space.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "space_id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id", "space_id"}}},
	{"name": "remove_from_space", "description": "Remove an object from a space.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "space_id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id", "space_id"}}},

	// Tags
	{"name": "add_tags", "description": "Add tags to an object.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "tags": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}}, "required": []interface{}{"id", "tags"}}},
	{"name": "remove_tag", "description": "Remove a tag from an object.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "tag": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id", "tag"}}},
	{"name": "list_tags", "description": "List all tags in your mind.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},

	// Spaces
	{"name": "list_spaces", "description": "List all spaces.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "create_space", "description": "Create a new space.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}}, "required": []interface{}{"name"}}},
	{"name": "get_space", "description": "Get a space by ID.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "delete_space", "description": "Delete a space by ID.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},

	// Related
	{"name": "related", "description": "Find objects semantically related to an object.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer", "default": 20}}, "required": []interface{}{"id"}}},
}

type jsonRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

func sendResponse(id interface{}, result interface{}) {
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}

func sendError(id interface{}, message string) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   map[string]interface{}{"code": -32600, "message": message},
	}
	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	accessKey, baseURL, err := loadAccessKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	client := NewMyMindClient(accessKey, baseURL)

	// Announce capabilities on startup
	sendResponse(nil, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]string{"name": "mymind", "version": version},
	})

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			sendResponse(req.ID, map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":   map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":     map[string]string{"name": "mymind", "version": version},
			})

		case "tools/list":
			sendResponse(req.ID, map[string]interface{}{"tools": toolManifest})

		case "tools/call":
			toolName, _ := req.Params["name"].(string)
			toolArgs, _ := req.Params["arguments"].(map[string]interface{})
			if toolArgs == nil {
				toolArgs = map[string]interface{}{}
			}

			handler, ok := tools[toolName]
			if !ok {
				sendError(req.ID, fmt.Sprintf("Unknown tool: %s", toolName))
				continue
			}

			result, err := handler(client, toolArgs)
			if err != nil {
				sendError(req.ID, err.Error())
				continue
			}

			resultJSON, _ := json.Marshal(result)
			sendResponse(req.ID, map[string]interface{}{
				"tool": toolName,
				"content": []map[string]interface{}{
					{"type": "text", "text": string(resultJSON)},
				},
			})

		default:
			sendError(req.ID, fmt.Sprintf("Unsupported method: %s", req.Method))
		}
	}
}