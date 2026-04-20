package service

import (
	"encoding/json"
	"errors"
)

type TaskValidator struct{}

func NewTaskValidator() *TaskValidator {
	return &TaskValidator{}
}

func (v *TaskValidator) Validate(taskType string, payload json.RawMessage) error {
	switch taskType {

	case "resize_image":
		var p struct {
			ImageURL string `json:"image_url"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return errors.New("invalid payload format for resize_image")
		}
		if p.ImageURL == "" {
			return errors.New("image_url is required")
		}
		if p.Width <= 0 || p.Height <= 0 {
			return errors.New("width and height must be > 0")
		}
		return nil

	case "scrape_url":
		var p struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return errors.New("invalid payload format for scrape_url")
		}
		if p.URL == "" {
			return errors.New("url is required")
		}
		return nil

	case "generate_report":
		var p struct {
			Date string `json:"date"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return errors.New("invalid payload format for generate_report")
		}
		if p.Date == "" {
			return errors.New("date is required")
		}
		return nil

	default:
		return errors.New("unsupported task type")
	}
}