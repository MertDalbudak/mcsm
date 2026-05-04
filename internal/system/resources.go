package system

// Resources is the cross-platform shape returned by /system/resources.
// Implementations live in resources_linux.go / resources_darwin.go and
// fill in whichever fields they can. Unsupported fields stay zero.
type Resources struct {
	CPU  CPU    `json:"cpu"`
	Mem  Memory `json:"mem"`
	Load Load   `json:"load"`
	Disk []Disk `json:"disk,omitempty"`
}

type CPU struct {
	Cores int     `json:"cores"`
	UsedP float64 `json:"used_pct"`        // 0–100
}

type Memory struct {
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	UsedP      float64 `json:"used_pct"`
}

type Load struct {
	One     float64 `json:"1m"`
	Five    float64 `json:"5m"`
	Fifteen float64 `json:"15m"`
}

type Disk struct {
	Mount      string  `json:"mount"`
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	UsedP      float64 `json:"used_pct"`
}
