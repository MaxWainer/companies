package model

//go:generate go tool easyjson -disable_members_unescape model.go

//easyjson:json
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
} //	@name	Error

//easyjson:json
type ErrorDetail struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
} //	@name	ErrorDetail
