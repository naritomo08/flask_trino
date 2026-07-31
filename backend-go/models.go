package main

type Filters struct {
	Date      string `json:"date"`
	TimeFrom  string `json:"time_from"`
	TimeTo    string `json:"time_to"`
	LogType   string `json:"log_type"`
	Host      string `json:"host"`
	Program   string `json:"program"`
	Message   string `json:"message"`
	Page      int    `json:"page"`
	Size      int    `json:"size"`
	SkipTotal bool   `json:"skip_total"`
}

type LogRecord map[string]any
