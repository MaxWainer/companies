package model

type ValidationFailedError struct {
	Error struct {
		Code      string            `json:"code"       example:"validation_failed" enums:"validation_failed,bad_request"`
		Message   string            `json:"message"    example:"request validation failed"`
		Fields    map[string]string `json:"fields"     example:"name:must be at most 15 characters,amount_of_employees:is required"`
		RequestID string            `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	ValidationFailedError

type InvalidIDError struct {
	Error struct {
		Code      string            `json:"code"       example:"validation_failed"`
		Message   string            `json:"message"    example:"request validation failed"`
		Fields    map[string]string `json:"fields"     example:"id:must be a valid UUID"`
		RequestID string            `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	InvalidIDError

type UnauthorizedError struct {
	Error struct {
		Code      string `json:"code"       example:"unauthorized"`
		Message   string `json:"message"    example:"missing bearer token" enums:"missing bearer token,invalid or expired token"`
		RequestID string `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	UnauthorizedError

type NotFoundError struct {
	Error struct {
		Code      string `json:"code"       example:"not_found"`
		Message   string `json:"message"    example:"company not found"`
		RequestID string `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	NotFoundError

type ConflictError struct {
	Error struct {
		Code      string            `json:"code"       example:"conflict"`
		Message   string            `json:"message"    example:"company name is already taken"`
		Fields    map[string]string `json:"fields"     example:"name:is already taken"`
		RequestID string            `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	ConflictError

type PayloadTooLargeError struct {
	Error struct {
		Code      string `json:"code"       example:"payload_too_large"`
		Message   string `json:"message"    example:"request body must not exceed 65536 bytes"`
		RequestID string `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	PayloadTooLargeError

type UnsupportedMediaTypeError struct {
	Error struct {
		Code      string `json:"code"       example:"unsupported_media_type"`
		Message   string `json:"message"    example:"Content-Type must be application/json"`
		RequestID string `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	UnsupportedMediaTypeError

type InternalError struct {
	Error struct {
		Code      string `json:"code"       example:"internal_error"`
		Message   string `json:"message"    example:"internal server error"`
		RequestID string `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	InternalError

type ServiceUnavailableError struct {
	Error struct {
		Code      string `json:"code"       example:"service_unavailable" enums:"service_unavailable,timeout"`
		Message   string `json:"message"    example:"the change was not saved because its event could not be published; retry later"`
		RequestID string `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	ServiceUnavailableError

type TimeoutError struct {
	Error struct {
		Code      string `json:"code"       example:"timeout"`
		Message   string `json:"message"    example:"request timed out; retry later"`
		RequestID string `json:"request_id" example:"6f1c2a52-3b0e-4b8e-9d3a-2f4c8e1d7a90"`
	} `json:"error"`
} //	@name	TimeoutError
