package kairosdb

type KairosdbQuery struct {
	StartAbsolute int64                    `json:"start_absolute"`
	EndAbsolute   int64                    `json:"end_absolute"`
	Queries       []map[string]interface{} `json:"queries"`
}

type KairosdbResponse struct {
	Metric     string             `json:"metric"`
	DataPoints map[string]float64 `json:"dps"`
}
