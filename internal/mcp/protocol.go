package mcp

import "encoding/json"

// JSON-RPC 2.0 message structures

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"` // can be string, number, or null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// MCP Protocol specific structures

type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

type ClientCapabilities struct {
	Roots        *RootsCapability       `json:"roots,omitempty"`
	Sampling     *SamplingCapability    `json:"sampling,omitempty"`
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type SamplingCapability struct{}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	Tools        *ToolsCapability       `json:"tools,omitempty"`
	Resources    *ResourcesCapability   `json:"resources,omitempty"`
	Prompts      *PromptsCapability     `json:"prompts,omitempty"`
	Logging      *LoggingCapability     `json:"logging,omitempty"`
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type LoggingCapability struct{}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool structures

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type Content struct {
	Type     string `json:"type"` // "text", "image", "resource"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Helper functions to create messages

func NewRequest(id interface{}, method string, params interface{}) (*JSONRPCRequest, error) {
	var paramsJSON json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		paramsJSON = b
	}
	return &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}, nil
}

func NewNotification(method string, params interface{}) (*JSONRPCNotification, error) {
	var paramsJSON json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		paramsJSON = b
	}
	return &JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
	}, nil
}

func NewResponse(id interface{}, result interface{}) (*JSONRPCResponse, error) {
	var resultJSON json.RawMessage
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		resultJSON = b
	}
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultJSON,
	}, nil
}

func NewErrorResponse(id interface{}, code int, message string, data interface{}) (*JSONRPCResponse, error) {
	var dataJSON json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		dataJSON = b
	}
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    dataJSON,
		},
	}, nil
}
