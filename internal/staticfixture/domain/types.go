package domain

type Request struct {
	Name string `json:"name" binding:"required"`
}

type Response struct {
	ID string `json:"id"`
}

type IDs []string
