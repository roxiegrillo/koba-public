package domain

type WebSocketInput struct {
	Message string `json:"message"`
}

type WebSocketOutput struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type KafkaEvent struct {
	ID string `json:"id"`
}

type Request struct {
	Name string `json:"name"`
}

type CreateRequest struct {
	Subject string
	Payload Request
}

type Response struct {
	ID string `json:"id"`
}

type IDs []string

type TagDetails struct {
	Code string `json:"code"`
}

type Tag struct {
	Name    string     `json:"name"`
	Kind    string     `json:"kind"`
	Details TagDetails `json:"details"`
}

type Configuration struct {
	Attributes map[string]any `json:"attributes,omitempty"`
	Tags       []Tag          `json:"tags,omitempty"`
}

type PageSize int

type ComposeRequest struct {
	ID       string
	PageSize *PageSize
	Payload  ComposePayload
}

type ComposePayload struct {
	Name      string         `json:"name"`
	Details   ComposeDetails `json:"details"`
	Latitude  *float64       `json:"lat,omitempty"`
	Longitude *float64       `json:"lon,omitempty"`
}

type ComposeDetails struct {
	Note string `json:"note"`
}
