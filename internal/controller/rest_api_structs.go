package controller

type FortigateSystemStatus struct {
	HTTPMethod string `json:"http_method"`
	Results    struct {
		ModelName     string `json:"model_name"`
		ModelNumber   string `json:"model_number"`
		Model         string `json:"model"`
		Hostname      string `json:"hostname"`
		LogDiskStatus string `json:"log_disk_status"`
	} `json:"results"`
	Vdom    string `json:"vdom"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Serial  string `json:"serial"`
	Version string `json:"version"`
	Build   int    `json:"build"`
}

type VIPListResponse struct {
	Status  string `json:"status"`
	Results []VIP  `json:"results"`
}

type MappedIP struct {
	Range      string `json:"range"`
	QOriginKey string `json:"q_origin_key"`
}

type VIP struct {
	Name     string     `json:"name"`
	ExtIP    string     `json:"extip"`
	MappedIP []MappedIP `json:"mappedip"`
	UUID     string     `json:"uuid"`
}

type apiResponse struct {
	Status  string `json:"status"`
	Serial  string `json:"serial"`
	Version string `json:"version"`
}
