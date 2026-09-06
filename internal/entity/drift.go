package entity

type DriftItem struct {
	DeviceID       string `json:"device_id"`
	DeviceName     string `json:"device_name"`
	Package        string `json:"package"`
	ExpectedSHA256 string `json:"expected_sha256"`
	ObservedSHA256 string `json:"observed_sha256"`
	Kind           string `json:"kind"`
}
