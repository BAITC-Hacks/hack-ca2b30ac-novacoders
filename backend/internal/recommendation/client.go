package recommendation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const MaxPayloadBytes = 50_000_000

// Error keeps structured validation errors from the service; arbitrary HTML or
// upstream internals are never exposed as a successful calculation.
type Error struct {
	HTTPStatus int
	Code       string          `json:"code"`
	Message    string          `json:"message"`
	RequestID  string          `json:"requestId,omitempty"`
	Fields     []FieldError    `json:"fields,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

const DefaultServiceDeadline = 30 * time.Second
const ResponseTimeAllowance = 5 * time.Second

type ClientConfig struct {
	BaseURL         string
	Token           string
	ServiceDeadline time.Duration
}

func NewClient(baseURL string) (*Client, error) {
	return NewConfiguredClient(ClientConfig{BaseURL: baseURL, ServiceDeadline: DefaultServiceDeadline})
}

func NewConfiguredClient(cfg ClientConfig) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("AI_SERVICE_URL должен быть HTTP(S) URL без credentials, query и fragment")
	}
	if cfg.ServiceDeadline <= 0 || cfg.ServiceDeadline > time.Minute {
		return nil, fmt.Errorf("AI_REQUEST_TIMEOUT_SECONDS должен быть больше 0 и не больше 60 для текущего HTTP deadline Go")
	}
	for _, c := range cfg.Token {
		if c <= ' ' || c >= 127 {
			return nil, fmt.Errorf("AI_SERVICE_TOKEN должен содержать только печатные ASCII-символы без пробелов")
		}
	}
	return &Client{endpoint: strings.TrimRight(cfg.BaseURL, "/") + "/v1/recommendations", token: cfg.Token, http: &http.Client{Timeout: cfg.ServiceDeadline + ResponseTimeAllowance, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type Result struct {
	Response Response
	Raw      json.RawMessage
}

func (c *Client) Recommend(ctx context.Context, input *Request) (*Result, error) {
	if !input.Settings.ExcludePartialMonth {
		return nil, &Error{HTTPStatus: 422, Code: "AI_UNSUPPORTED_SETTINGS", Message: "Текущая версия AI Service рассчитывает только завершённые месяцы. Включите «Исключать неполный месяц».", RequestID: input.RequestID, Fields: []FieldError{{Field: "settings.excludePartialMonth", Message: "AI Service требует true; значение false пока не поддержано алгоритмом."}}}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxPayloadBytes {
		return nil, &Error{HTTPStatus: 413, Code: "AI_PAYLOAD_TOO_LARGE", Message: "Нормализованный JSON превышает 50 MB.", RequestID: input.RequestID}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, &Error{HTTPStatus: 503, Code: "AI_SERVICE_UNAVAILABLE", Message: "AI Service недоступен. Импорт сохранён; повторите создание расчёта после запуска сервиса.", RequestID: input.RequestID}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxPayloadBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, invalid(input.RequestID, "Не удалось прочитать ответ AI Service.")
	}
	if len(raw) > MaxPayloadBytes {
		return nil, invalid(input.RequestID, "Ответ AI Service превышает 50 MB.")
	}
	media, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if media != "application/json" {
		return nil, invalid(input.RequestID, "AI Service вернул не JSON.")
	}
	if res.StatusCode != http.StatusOK {
		var body struct {
			RequestID string `json:"requestId"`
			Error     struct {
				Code    string       `json:"code"`
				Message string       `json:"message"`
				Fields  []FieldError `json:"fields"`
			} `json:"error"`
		}
		status := 502
		if res.StatusCode == 400 || res.StatusCode == 422 {
			status = 422
		}
		if res.StatusCode == 413 {
			status = 413
		}
		e := &Error{HTTPStatus: status, Code: "AI_SERVICE_ERROR", Message: "AI Service не смог выполнить расчёт.", RequestID: input.RequestID}
		switch res.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			e.Code, e.Message = "AI_AUTHENTICATION_FAILED", "AI Service отклонил внутренний токен. Проверьте AI_SERVICE_TOKEN в окружении Go и AI Service."
		case http.StatusServiceUnavailable:
			e.HTTPStatus, e.Code, e.Message = 503, "AI_SERVICE_UNAVAILABLE", "AI Service недоступен или не настроен. Импорт сохранён; повторите расчёт после настройки сервиса."
		case http.StatusGatewayTimeout:
			e.HTTPStatus, e.Code, e.Message = 504, "AI_REQUEST_TIMEOUT", "AI Service исчерпал время обработки. Импорт сохранён; уменьшите набор или увеличьте согласованный deadline."
		}
		if json.Unmarshal(raw, &body) == nil && (res.StatusCode == 400 || res.StatusCode == 422 || res.StatusCode == 413) {
			e.Code = "AI_" + body.Error.Code
			e.Fields = body.Error.Fields
			if body.Error.Message != "" {
				e.Message = body.Error.Message
			}
		}
		return nil, e
	}
	var output Response
	if json.Unmarshal(raw, &output) != nil {
		return nil, invalid(input.RequestID, "Некорректный JSON ответа AI Service.")
	}
	if err := output.Validate(input); err != nil {
		var incomplete *Error
		if errors.As(err, &incomplete) {
			// Preserve the validated AI result, including nulls, calculation and
			// explanation. The current frontend cannot render a nullable order.
			incomplete.Result = raw
			return nil, incomplete
		}
		return nil, invalid(input.RequestID, err.Error())
	}
	return &Result{Response: output, Raw: raw}, nil
}
func invalid(id, msg string) *Error {
	return &Error{HTTPStatus: 502, Code: "AI_INVALID_RESPONSE", Message: msg, RequestID: id}
}
