package domain

type Request struct {
	Name string `json:"name"`
}

type Response struct {
	ID string `json:"id"`
}

type IDs []string

type PageSize int

type ComposeRequest struct {
	ID       string
	PageSize *PageSize
	Payload  ComposePayload
}

type ComposePayload struct {
	Name    string         `json:"name"`
	Details ComposeDetails `json:"details"`
}

type ComposeDetails struct {
	Note string `json:"note"`
}
