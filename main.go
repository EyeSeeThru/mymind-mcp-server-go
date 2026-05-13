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

	"github.com/EyeSeeThru/mymind-mcp-server-go/jwt"
)

// ─── Version ────────────────────────────────────────────────────────────────

const version = "1.2.0"
const baseURL = "https://api.mymind.com"

// ─── Credential Loading ───────────────────────────────────────────────────────

func loadAccessKey() (accessKey string, baseURLOut string, err error) {
	baseURLOut = baseURL

	// 1. Env var
	if key := os.Getenv("MYMIND_ACCESS_KEY"); key != "" {
		return key, baseURLOut, nil
	}

	// 2. Default key file
	keyPath := filepath.Join(os.Getenv("HOME"), ".mymind_mcp_access_key")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data)), baseURLOut, nil
	}

	// 3. YAML config
	configPath := filepath.Join(os.Getenv("HOME"), ".mymind_mcp_config.yaml")
	if data, err := os.ReadFile(configPath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "access_key:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1]), baseURLOut, nil
				}
			}
			if strings.HasPrefix(line, "base_url:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					baseURLOut = strings.TrimSpace(parts[1])
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

func (c *MyMindClient) requestRaw(method, path string, headers map[string]string) ([]byte, string, error) {
	reqURL := c.baseURL + path

	req, err := http.NewRequest(method, reqURL, nil)
	if err != nil {
		return nil, "", err
	}

	token := jwt.Sign(c.kid, c.secret, path, method)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "mymind-mcp-server-go/"+version)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

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

func (c *MyMindClient) ListObjects(q string, limit int, contentAs string, similarTo string) ([]interface{}, error) {
	params := map[string]string{"limit": fmt.Sprintf("%d", limit)}
	if q != "" {
		params["q"] = q
	}
	if similarTo != "" {
		params["similarTo"] = similarTo
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

func (c *MyMindClient) UpdateObject(id, title, summary string) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if title != "" {
		body["title"] = title
	}
	if summary != "" {
		body["summary"] = summary
	}
	return c.request("PATCH", "/objects/"+id, body, nil)
}

func (c *MyMindClient) DeleteObject(id string) (map[string]interface{}, error) {
	return c.request("DELETE", "/objects/"+id, nil, nil)
}

func (c *MyMindClient) RestoreObject(id string) (map[string]interface{}, error) {
	return c.request("POST", "/objects/"+id+"/restore", nil, nil)
}

func (c *MyMindClient) DownloadObject(id string) (map[string]interface{}, error) {
	raw, contentType, err := c.requestRaw("GET", "/objects/"+id+"/blob", nil)
	if err != nil {
		return nil, err
	}
	dataB64 := base64.StdEncoding.EncodeToString(raw)
	return map[string]interface{}{
		"content_type": contentType,
		"data":         dataB64,
	}, nil
}

func (c *MyMindClient) GetContent(id, accept string) (map[string]interface{}, error) {
	headers := map[string]string{"Accept": accept}
	raw, contentType, err := c.requestRaw("GET", "/objects/"+id+"/content", headers)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"content_type": contentType,
		"content":      string(raw),
	}, nil
}

func (c *MyMindClient) PinObject(id string, position int) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if position > 0 {
		body["position"] = position
	}
	return c.request("POST", "/objects/"+id+"/pin", body, nil)
}

func (c *MyMindClient) UnpinObject(id string) (map[string]interface{}, error) {
	return c.request("DELETE", "/objects/"+id+"/pin", nil, nil)
}

// ─── Search ─────────────────────────────────────────────────────────────────

func (c *MyMindClient) Search(query string, limit int, semantic bool, semanticBoost float64, similarTo string, rerank bool) ([]interface{}, error) {
	params := map[string]string{
		"q":     query,
		"limit": fmt.Sprintf("%d", limit),
	}
	if semantic {
		params["semantic"] = "true"
	}
	if semanticBoost != 0 {
		params["semanticBoost"] = fmt.Sprintf("%f", semanticBoost)
	}
	if similarTo != "" {
		params["similarTo"] = similarTo
		params["semantic"] = "true"
	}
	if rerank {
		params["rerank"] = "true"
		params["semantic"] = "true"
	}
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

// ─── Blob / Thumbnail / Screenshot ──────────────────────────────────────────

func (c *MyMindClient) GetBlob(id string) (map[string]interface{}, error) {
	raw, contentType, err := c.requestRaw("GET", "/objects/"+id+"/blob", nil)
	if err != nil {
		return nil, err
	}
	dataB64 := base64.StdEncoding.EncodeToString(raw)
	return map[string]interface{}{
		"content_type": contentType,
		"data":         dataB64,
	}, nil
}

func (c *MyMindClient) GetThumbnail(id string, size string) (map[string]interface{}, error) {
	params := map[string]string{}
	if size != "" {
		params["size"] = size
	}
	path := "/objects/" + id + "/thumbnail"
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		path += "?" + q.Encode()
	}
	raw, contentType, err := c.requestRaw("GET", path, nil)
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
	raw, contentType, err := c.requestRaw("GET", "/objects/"+id+"/screenshot", nil)
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

func (c *MyMindClient) Convert(content, fromType, toType string) (map[string]interface{}, error) {
	headers := map[string]string{
		"Content-Type": fromType,
		"Accept":       toType,
	}
	reqURL := c.baseURL + "/convert"
	var bodyReader io.Reader
	if fromType == "text/plain" {
		bodyReader = strings.NewReader(content)
	} else {
		data, _ := json.Marshal(content)
		bodyReader = strings.NewReader(string(data))
	}

	path := "/convert"
	req, err := http.NewRequest("POST", reqURL, bodyReader)
	if err != nil {
		return nil, err
	}
	token := jwt.Sign(c.kid, c.secret, path, "POST")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "mymind-mcp-server-go/"+version)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

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

// ─── Notes ───────────────────────────────────────────────────────────────────

func (c *MyMindClient) CreateNote(objectID, content string, contentType string) (map[string]interface{}, error) {
	headers := map[string]string{"Content-Type": contentType}
	path := "/objects/" + objectID + "/notes"
	var bodyReader io.Reader
	if contentType == "text/markdown" {
		data, _ := json.Marshal(map[string]string{"content": content})
		bodyReader = strings.NewReader(string(data))
	} else {
		bodyReader = strings.NewReader(content)
	}

	req, err := http.NewRequest("POST", c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	token := jwt.Sign(c.kid, c.secret, path, "POST")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "mymind-mcp-server-go/"+version)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

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

func (c *MyMindClient) UpdateNote(objectID, noteID, content string, contentType string) (map[string]interface{}, error) {
	headers := map[string]string{"Content-Type": contentType}
	path := "/objects/" + objectID + "/notes/" + noteID
	var bodyReader io.Reader
	if contentType == "text/markdown" {
		data, _ := json.Marshal(map[string]string{"content": content})
		bodyReader = strings.NewReader(string(data))
	} else {
		bodyReader = strings.NewReader(content)
	}

	req, err := http.NewRequest("PUT", c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	token := jwt.Sign(c.kid, c.secret, path, "PUT")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "mymind-mcp-server-go/"+version)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

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

func (c *MyMindClient) DeleteNote(objectID, noteID string) (map[string]interface{}, error) {
	return c.request("DELETE", "/objects/"+objectID+"/notes/"+noteID, nil, nil)
}

// ─── Links ──────────────────────────────────────────────────────────────────

func (c *MyMindClient) ListLinks() ([]interface{}, error) {
	resp, err := c.request("GET", "/links", nil, nil)
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

func (c *MyMindClient) CreateLink(sourceID, targetID string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"sourceId": sourceID,
		"targetId": targetID,
	}
	return c.request("POST", "/links", body, nil)
}

func (c *MyMindClient) DeleteLink(linkID string) (map[string]interface{}, error) {
	return c.request("DELETE", "/links/"+linkID, nil, nil)
}

// ─── Space Membership ───────────────────────────────────────────────────────

func (c *MyMindClient) AddToSpace(spaceID, objectID string) (map[string]interface{}, error) {
	return c.request("PUT", "/spaces/"+spaceID+"/objects/"+objectID, nil, nil)
}

func (c *MyMindClient) RemoveFromSpace(spaceID, objectID string) (map[string]interface{}, error) {
	return c.request("DELETE", "/spaces/"+spaceID+"/objects/"+objectID, nil, nil)
}

// ─── Tags ────────────────────────────────────────────────────────────────────

func (c *MyMindClient) AddTags(id string, tags []string) (map[string]interface{}, error) {
	body := make([]map[string]string, len(tags))
	for i, t := range tags {
		body[i] = map[string]string{"name": t}
	}
	return c.request("POST", "/objects/"+id+"/tags", body, nil)
}

func (c *MyMindClient) RemoveTags(id string, tags []map[string]string) (map[string]interface{}, error) {
	return c.request("DELETE", "/objects/"+id+"/tags", tags, nil)
}

func (c *MyMindClient) ListTags(limit int) ([]interface{}, error) {
	params := map[string]string{"limit": fmt.Sprintf("%d", limit)}
	resp, err := c.request("GET", "/tags", nil, params)
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

func (c *MyMindClient) CreateSpace(name string, color string) (map[string]interface{}, error) {
	body := map[string]interface{}{"name": name}
	if color != "" {
		body["color"] = color
	}
	return c.request("POST", "/spaces", body, nil)
}

func (c *MyMindClient) GetSpace(id string) (map[string]interface{}, error) {
	return c.request("GET", "/spaces/"+id, nil, nil)
}

func (c *MyMindClient) UpdateSpace(id, name, color string) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if name != "" {
		body["name"] = name
	}
	if color != "" {
		body["color"] = color
	}
	return c.request("PATCH", "/spaces/"+id, body, nil)
}

func (c *MyMindClient) DeleteSpace(id string) (map[string]interface{}, error) {
	return c.request("DELETE", "/spaces/"+id, nil, nil)
}

// ─── Related (fixed: use /search?similarTo=) ─────────────────────────────────

func (c *MyMindClient) Related(id string, limit int) ([]interface{}, error) {
	params := map[string]string{
		"similarTo": id,
		"semantic":  "true",
		"limit":     fmt.Sprintf("%d", limit),
	}
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

// ─── Tool Handlers ───────────────────────────────────────────────────────────

type toolHandler func(client *MyMindClient, args map[string]interface{}) (interface{}, error)

var tools = map[string]toolHandler{
	// Objects
	"list_objects": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.ListObjects(getString(args, "q"), getInt(args, "limit", 100), getString(args, "contentAs"), getString(args, "similarTo"))
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
		return c.UpdateObject(getString(args, "id"), getString(args, "title"), getString(args, "summary"))
	},
	"delete_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.DeleteObject(getString(args, "id"))
	},
	"restore_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.RestoreObject(getString(args, "id"))
	},
	"download_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.DownloadObject(getString(args, "id"))
	},
	"get_content": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetContent(getString(args, "id"), getString(args, "accept"))
	},
	"pin_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.PinObject(getString(args, "id"), getInt(args, "position", 0))
	},
	"unpin_object": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.UnpinObject(getString(args, "id"))
	},

	// Search
	"search": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.Search(
			getString(args, "query"),
			getInt(args, "limit", 20),
			getBool(args, "semantic"),
			getFloat(args, "semanticBoost"),
			getString(args, "similarTo"),
			getBool(args, "rerank"),
		)
	},

	// Blob / Thumbnail / Screenshot
	"get_blob": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetBlob(getString(args, "id"))
	},
	"get_thumbnail": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetThumbnail(getString(args, "id"), getString(args, "size"))
	},
	"get_screenshot": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetScreenshot(getString(args, "id"))
	},

	// Convert
	"convert": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.Convert(getString(args, "content"), getString(args, "from"), getString(args, "to"))
	},

	// Notes CRUD
	"create_note": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.CreateNote(getString(args, "objectId"), getString(args, "content"), getString(args, "contentType"))
	},
	"update_note": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.UpdateNote(getString(args, "objectId"), getString(args, "noteId"), getString(args, "content"), getString(args, "contentType"))
	},
	"delete_note": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.DeleteNote(getString(args, "objectId"), getString(args, "noteId"))
	},

	// Links
	"list_links": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.ListLinks()
	},
	"create_link": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.CreateLink(getString(args, "sourceId"), getString(args, "targetId"))
	},
	"delete_link": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.DeleteLink(getString(args, "id"))
	},

	// Space membership
	"add_object_to_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.AddToSpace(getString(args, "spaceId"), getString(args, "objectId"))
	},
	"remove_object_from_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.RemoveFromSpace(getString(args, "spaceId"), getString(args, "objectId"))
	},

	// Tags
	"add_tags": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.AddTags(getString(args, "id"), toStringArray(args["tags"]))
	},
	"remove_tags": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		tagsRaw, ok := args["tags"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("tags must be an array")
		}
		tags := make([]map[string]string, len(tagsRaw))
		for i, t := range tagsRaw {
			if m, ok := t.(map[string]interface{}); ok {
				tags[i] = map[string]string{}
				if name, ok := m["name"].(string); ok {
					tags[i]["name"] = name
				}
				if id, ok := m["id"].(string); ok {
					tags[i]["id"] = id
				}
			}
		}
		return c.RemoveTags(getString(args, "id"), tags)
	},
	"list_tags": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.ListTags(getInt(args, "limit", 1000))
	},

	// Spaces
	"list_spaces": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.ListSpaces()
	},
	"create_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.CreateSpace(getString(args, "name"), getString(args, "color"))
	},
	"get_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.GetSpace(getString(args, "id"))
	},
	"update_space": func(c *MyMindClient, args map[string]interface{}) (interface{}, error) {
		return c.UpdateSpace(getString(args, "id"), getString(args, "name"), getString(args, "color"))
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

func getFloat(args map[string]interface{}, key string) float64 {
	if v, ok := args[key].(float64); ok {
		return v
	}
	return 0
}

func getBool(args map[string]interface{}, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
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
	{"name": "list_objects", "description": "List objects from MyMind. Params: q, limit, contentAs, similarTo.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer", "default": 100}, "contentAs": map[string]interface{}{"type": "string"}, "similarTo": map[string]interface{}{"type": "string"}}}},
	{"name": "create_object", "description": "Create a new object (URL, note, or content) in MyMind.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"title": map[string]interface{}{"type": "string"}, "content": map[string]interface{}{"type": "string"}, "url": map[string]interface{}{"type": "string"}, "tags": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}, "spaces": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}}, "required": []interface{}{}}},
	{"name": "get_object", "description": "Get a single object by ID. Optional: contentAs.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "contentAs": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "update_object", "description": "Update an object's title or summary.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "title": map[string]interface{}{"type": "string"}, "summary": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "delete_object", "description": "Delete an object by ID.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "restore_object", "description": "Restore a soft-deleted object within the 30-day recovery window.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "download_object", "description": "Download original uploaded bytes (base64). Uses /blob path.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "get_content", "description": "Get raw object content. Params: id, accept (text/plain | text/markdown | application/prose+json | text/html).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "accept": map[string]interface{}{"type": "string", "default": "text/plain"}}, "required": []interface{}{"id"}}},
	{"name": "pin_object", "description": "Pin an object to top of mind. Optional: position (zero-based slot).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "position": map[string]interface{}{"type": "integer"}}, "required": []interface{}{"id"}}},
	{"name": "unpin_object", "description": "Unpin an object.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},

	// Search
	{"name": "search", "description": "Search MyMind objects. Params: query, limit, semantic, semanticBoost, similarTo, rerank.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer", "default": 20}, "semantic": map[string]interface{}{"type": "boolean"}, "semanticBoost": map[string]interface{}{"type": "number"}, "similarTo": map[string]interface{}{"type": "string"}, "rerank": map[string]interface{}{"type": "boolean"}}, "required": []interface{}{"query"}}},

	// Blob / Thumbnail / Screenshot
	{"name": "get_blob", "description": "Get the raw blob data for an object (base64).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "get_thumbnail", "description": "Get the thumbnail image for an object (base64). Optional: size (WxH).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "size": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "get_screenshot", "description": "Get the screenshot image for an object (base64).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},

	// Convert
	{"name": "convert", "description": "Convert content between text/plain, text/markdown, application/prose+json. Params: content, from, to.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"content": map[string]interface{}{"type": "string"}, "from": map[string]interface{}{"type": "string"}, "to": map[string]interface{}{"type": "string"}}, "required": []interface{}{"content", "from", "to"}}},

	// Notes CRUD
	{"name": "create_note", "description": "Append a note to an object. Params: objectId, content, contentType (default text/markdown).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"objectId": map[string]interface{}{"type": "string"}, "content": map[string]interface{}{"type": "string"}, "contentType": map[string]interface{}{"type": "string", "default": "text/markdown"}}, "required": []interface{}{"objectId", "content"}}},
	{"name": "update_note", "description": "Replace a note's body. Params: objectId, noteId, content, contentType.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"objectId": map[string]interface{}{"type": "string"}, "noteId": map[string]interface{}{"type": "string"}, "content": map[string]interface{}{"type": "string"}, "contentType": map[string]interface{}{"type": "string", "default": "text/markdown"}}, "required": []interface{}{"objectId", "noteId", "content"}}},
	{"name": "delete_note", "description": "Remove a note from an object. Idempotent.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"objectId": map[string]interface{}{"type": "string"}, "noteId": map[string]interface{}{"type": "string"}}, "required": []interface{}{"objectId", "noteId"}}},

	// Links
	{"name": "list_links", "description": "List all links (WikiLink and Manual) in your mind.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "create_link", "description": "Create a Manual link between two objects. Params: sourceId, targetId.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"sourceId": map[string]interface{}{"type": "string"}, "targetId": map[string]interface{}{"type": "string"}}, "required": []interface{}{"sourceId", "targetId"}}},
	{"name": "delete_link", "description": "Delete a Manual link by ID. WikiLinks return 422.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},

	// Space membership
	{"name": "add_object_to_space", "description": "Add an object to a space. Idempotent.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"spaceId": map[string]interface{}{"type": "string"}, "objectId": map[string]interface{}{"type": "string"}}, "required": []interface{}{"spaceId", "objectId"}}},
	{"name": "remove_object_from_space", "description": "Remove an object from a space. Idempotent.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"spaceId": map[string]interface{}{"type": "string"}, "objectId": map[string]interface{}{"type": "string"}}, "required": []interface{}{"spaceId", "objectId"}}},

	// Tags
	{"name": "add_tags", "description": "Add tags to an object. Params: id, tags (array of strings).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "tags": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}}, "required": []interface{}{"id", "tags"}}},
	{"name": "remove_tags", "description": "Remove tags from an object. Params: id, tags (array of {name} or {id} objects).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "tags": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}}}, "required": []interface{}{"id", "tags"}}},
	{"name": "list_tags", "description": "List all tags in your mind.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"limit": map[string]interface{}{"type": "integer", "default": 1000}}}},

	// Spaces
	{"name": "list_spaces", "description": "List all spaces.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	{"name": "create_space", "description": "Create a new space. Params: name, color (optional CSS color).", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}, "color": map[string]interface{}{"type": "string"}}, "required": []interface{}{"name"}}},
	{"name": "get_space", "description": "Get a space by ID.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "update_space", "description": "Update a space's name or color.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "name": map[string]interface{}{"type": "string"}, "color": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},
	{"name": "delete_space", "description": "Delete a space. Objects in the space survive.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []interface{}{"id"}}},

	// Related
	{"name": "related", "description": "Find objects semantically related to an object. Uses /search?similarTo= internally.", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer", "default": 20}}, "required": []interface{}{"id"}}},
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
	accessKey, baseURLOut, err := loadAccessKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	client := NewMyMindClient(accessKey, baseURLOut)

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
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]string{"name": "mymind", "version": version},
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
